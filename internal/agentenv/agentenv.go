// Package agentenv reads and writes agent.env, the file that holds one
// agent's credential and proxy settings. The file is read by a shell, so
// every value written to it is checked against a strict set of characters
// first, the same sets the connect script uses.
package agentenv

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// FileName is the name of the agent's settings file.
	FileName = "agent.env"
	// CAFileName is the gateway's certificate, next to agent.env.
	CAFileName = "gateway-ca.pem"
	// CredentialVar names the agent's credential in the environment.
	CredentialVar = "ONECLI_AGENT_CREDENTIAL"
	// HomeVar names the folder for agent.env.
	HomeVar = "ONECLI_HOME"
	// ProfileVar names the shell profile that reads agent.env.
	ProfileVar = "ONECLI_PROFILE"
	// CredentialPrefix starts every agent credential.
	CredentialPrefix = "oc_agent_"
	// ConnectTokenPrefix starts every connect token.
	ConnectTokenPrefix = "oc_connect_"
)

var (
	plainRE   = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	addressRE = regexp.MustCompile(`^[A-Za-z0-9.:-]+$`)
	idRE      = regexp.MustCompile(`^[A-Za-z0-9_-]*$`)
	dirRE     = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	lineRE    = regexp.MustCompile(`^export ([A-Za-z_][A-Za-z0-9_]*)='([^']*)'$`)
)

// Plain reports whether s holds only letters, digits and underscores.
func Plain(s string) bool { return plainRE.MatchString(s) }

// ValidCredential reports whether s looks like an agent credential that is
// safe to put in a header and a file.
func ValidCredential(s string) bool {
	return len(s) > len(CredentialPrefix) && strings.HasPrefix(s, CredentialPrefix) && Plain(s)
}

// ValidConnectToken reports whether s looks like a connect token.
func ValidConnectToken(s string) bool {
	return len(s) > len(ConnectTokenPrefix) && strings.HasPrefix(s, ConnectTokenPrefix) && Plain(s)
}

// Dir returns the folder for agent.env: ONECLI_HOME, then /etc/onecli for
// root, then ~/.config/onecli. It must be absolute and plainly safe.
func Dir() (string, error) {
	dir := os.Getenv(HomeVar)
	if dir == "" {
		if os.Geteuid() == 0 {
			dir = "/etc/onecli"
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolving home dir: %w", err)
			}
			dir = filepath.Join(home, ".config", "onecli")
		}
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("the folder for agent.env must be an absolute path")
	}
	if !dirRE.MatchString(filepath.ToSlash(dir)) {
		return "", errors.New("the folder for agent.env has a character that is not safe to write")
	}
	return dir, nil
}

// Settings is what agent.env holds.
type Settings struct {
	AgentID    string
	VaultID    string
	Credential string
	Scheme     string
	Address    string
	CAFile     string
}

// Check refuses any value that is not plainly safe inside single quotes.
func (s Settings) Check() error {
	if !ValidCredential(s.Credential) {
		return errors.New("the server's answer had no agent credential that is safe to write")
	}
	if s.Scheme != "http" && s.Scheme != "https" {
		return errors.New("the server's answer had a gateway scheme that is not http or https")
	}
	if s.Address == "" || !addressRE.MatchString(s.Address) {
		return errors.New("the server's answer had a gateway address that is not safe to write")
	}
	if !idRE.MatchString(s.AgentID) || !idRE.MatchString(s.VaultID) {
		return errors.New("the server's answer had an id that is not safe to write")
	}
	if s.CAFile == "" || !dirRE.MatchString(filepath.ToSlash(s.CAFile)) {
		return errors.New("the certificate path is not safe to write")
	}
	return nil
}

// ProxyURL is the gateway proxy URL with the credential inside.
func (s Settings) ProxyURL() string {
	return s.Scheme + "://x:" + s.Credential + "@" + s.Address
}

// Pairs returns the exported variables in file order.
func (s Settings) Pairs() [][2]string {
	proxy := s.ProxyURL()
	var out [][2]string
	if s.AgentID != "" {
		out = append(out, [2]string{"ONECLI_AGENT_ID", s.AgentID})
		out = append(out, [2]string{"ONECLI_VAULT_ID", s.VaultID})
	}
	out = append(out,
		[2]string{CredentialVar, s.Credential},
		[2]string{"HTTPS_PROXY", proxy},
		[2]string{"HTTP_PROXY", proxy},
		[2]string{"https_proxy", proxy},
		[2]string{"http_proxy", proxy},
		[2]string{"NO_PROXY", "localhost,127.0.0.1"},
		[2]string{"no_proxy", "localhost,127.0.0.1"},
		[2]string{"NODE_EXTRA_CA_CERTS", s.CAFile},
		[2]string{"NODE_USE_ENV_PROXY", "1"},
		[2]string{"SSL_CERT_FILE", s.CAFile},
		[2]string{"CURL_CA_BUNDLE", s.CAFile},
		[2]string{"REQUESTS_CA_BUNDLE", s.CAFile},
		[2]string{"GIT_SSL_CAINFO", s.CAFile},
		[2]string{"PGSSLROOTCERT", s.CAFile},
	)
	return out
}

// Render returns the file's text. Check must pass first.
func (s Settings) Render() string {
	var b strings.Builder
	b.WriteString("# Written by onecli. Holds this agent's credential.\n")
	for _, kv := range s.Pairs() {
		b.WriteString("export " + kv[0] + "='" + kv[1] + "'\n")
	}
	return b.String()
}

// WriteFileFresh writes data to path through a fresh file made next to it
// (never written through an existing file or link), then renames it in.
func WriteFileFresh(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".new"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clearing %s: %w", tmp, err)
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("setting mode on %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("moving %s into place: %w", path, err)
	}
	return nil
}

// Write checks s and writes agent.env in dir, mode 0600.
func Write(dir string, s Settings) (string, error) {
	if err := s.Check(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, FileName)
	if err := WriteFileFresh(path, []byte(s.Render()), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Read parses agent.env. Only lines of the form export NAME='value' count.
func Read(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	vals := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := lineRE.FindStringSubmatch(strings.TrimSpace(sc.Text()))
		if m != nil {
			vals[m[1]] = m[2]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return vals, nil
}

// Source tells where a credential came from.
type Source string

const (
	SourceEnv  Source = "env"
	SourceFile Source = "file"
	SourceNone Source = ""
)

// Credential returns the agent's credential: ONECLI_AGENT_CREDENTIAL, then
// agent.env in dir. A value that does not look like a credential is ignored.
func Credential(dir string) (string, Source) {
	if v := os.Getenv(CredentialVar); ValidCredential(v) {
		return v, SourceEnv
	}
	vals, err := Read(filepath.Join(dir, FileName))
	if err != nil {
		return "", SourceNone
	}
	if v := vals[CredentialVar]; ValidCredential(v) {
		return v, SourceFile
	}
	return "", SourceNone
}

// HeldCredential is the credential agent.env in dir already holds, if any.
func HeldCredential(dir string) string {
	vals, err := Read(filepath.Join(dir, FileName))
	if err != nil {
		return ""
	}
	if v := vals[CredentialVar]; ValidCredential(v) {
		return v
	}
	return ""
}

// ProfileLine is the one line that reads agent.env in every new shell.
func ProfileLine(envFile string) string {
	return "[ -r '" + envFile + "' ] && . '" + envFile + "'"
}

// ProfilePath returns ONECLI_PROFILE, then /etc/profile.d/onecli.sh for
// root, then ~/.profile.
func ProfilePath() (string, error) {
	if p := os.Getenv(ProfileVar); p != "" {
		return p, nil
	}
	if os.Geteuid() == 0 {
		if st, err := os.Stat("/etc/profile.d"); err == nil && st.IsDir() {
			return "/etc/profile.d/onecli.sh", nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	return filepath.Join(home, ".profile"), nil
}

// EnsureProfileLine appends the line that reads envFile to profile, once.
// Reports whether the file changed.
func EnsureProfileLine(profile, envFile string) (bool, error) {
	line := ProfileLine(envFile)
	data, err := os.ReadFile(profile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("reading %s: %w", profile, err)
	}
	for _, l := range strings.Split(string(data), "\n") {
		if l == line {
			return false, nil
		}
	}
	f, err := os.OpenFile(profile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return false, fmt.Errorf("opening %s: %w", profile, err)
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	if _, err := f.WriteString(prefix + line + "\n"); err != nil {
		return false, fmt.Errorf("writing %s: %w", profile, err)
	}
	return true, nil
}

// RemoveProfileLine removes the line that reads envFile from profile.
// Reports whether the file changed.
func RemoveProfileLine(profile, envFile string) (bool, error) {
	data, err := os.ReadFile(profile)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", profile, err)
	}
	line := ProfileLine(envFile)
	lines := strings.Split(string(data), "\n")
	kept := lines[:0]
	changed := false
	for _, l := range lines {
		if l == line {
			changed = true
			continue
		}
		kept = append(kept, l)
	}
	if !changed {
		return false, nil
	}
	st, err := os.Stat(profile)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(profile, []byte(strings.Join(kept, "\n")), st.Mode().Perm()); err != nil {
		return false, fmt.Errorf("writing %s: %w", profile, err)
	}
	return true, nil
}
