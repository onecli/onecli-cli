package agentenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func good() Settings {
	return Settings{
		AgentID:    "agt_1",
		VaultID:    "vlt_1",
		Credential: "oc_agent_abc_DEF123",
		Scheme:     "http",
		Address:    "gateway.example.com:10255",
		CAFile:     "/tmp/onecli/gateway-ca.pem",
	}
}

func TestCheckRefusesUnsafeValues(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Settings)
	}{
		{"credential with a quote", func(s *Settings) { s.Credential = "oc_agent_a'b" }},
		{"credential with a space", func(s *Settings) { s.Credential = "oc_agent_a b" }},
		{"credential with a dollar", func(s *Settings) { s.Credential = "oc_agent_$(id)" }},
		{"credential with no prefix", func(s *Settings) { s.Credential = "oc_key_abc" }},
		{"empty credential", func(s *Settings) { s.Credential = "" }},
		{"scheme ftp", func(s *Settings) { s.Scheme = "ftp" }},
		{"scheme with quote", func(s *Settings) { s.Scheme = "http'" }},
		{"address with slash", func(s *Settings) { s.Address = "evil.com/x" }},
		{"address with at", func(s *Settings) { s.Address = "a@b:1" }},
		{"address with quote", func(s *Settings) { s.Address = "a':1" }},
		{"empty address", func(s *Settings) { s.Address = "" }},
		{"agent id with quote", func(s *Settings) { s.AgentID = "a'b" }},
		{"vault id with newline", func(s *Settings) { s.VaultID = "a\nb" }},
		{"ca path with quote", func(s *Settings) { s.CAFile = "/tmp/a'b" }},
		{"ca path with space", func(s *Settings) { s.CAFile = "/tmp/a b" }},
	}
	if err := good().Check(); err != nil {
		t.Fatalf("good settings refused: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := good()
			tc.edit(&s)
			if err := s.Check(); err == nil {
				t.Fatalf("Check accepted %+v", s)
			}
		})
	}
}

func TestWriteIsMode0600AndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path, err := Write(dir, good())
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", st.Mode().Perm())
	}
	vals, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ONECLI_AGENT_ID":         "agt_1",
		"ONECLI_VAULT_ID":         "vlt_1",
		"ONECLI_AGENT_CREDENTIAL": "oc_agent_abc_DEF123",
		"HTTPS_PROXY":             "http://x:oc_agent_abc_DEF123@gateway.example.com:10255",
		"http_proxy":              "http://x:oc_agent_abc_DEF123@gateway.example.com:10255",
		"NO_PROXY":                "localhost,127.0.0.1",
		"SSL_CERT_FILE":           "/tmp/onecli/gateway-ca.pem",
		"NODE_USE_ENV_PROXY":      "1",
	}
	for k, v := range want {
		if vals[k] != v {
			t.Errorf("%s = %q, want %q", k, vals[k], v)
		}
	}
}

func TestWriteRefusesAndLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	s := good()
	s.Credential = "oc_agent_x';rm -rf ~;'"
	if _, err := Write(dir, s); err == nil {
		t.Fatal("Write accepted an unsafe credential")
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); !os.IsNotExist(err) {
		t.Fatalf("agent.env exists after a refusal: %v", err)
	}
}

func TestWriteDoesNotFollowALinkAtTheTempName(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "stolen")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, FileName+".new")); err != nil {
		t.Skip("symlinks not available")
	}
	if _, err := Write(dir, good()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "x" {
		t.Fatalf("the link target was written through: %q", data)
	}
}

func TestWriteReplacesALinkAtAgentEnv(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "stolen")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, FileName)); err != nil {
		t.Skip("symlinks not available")
	}
	if _, err := Write(dir, good()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "x" {
		t.Fatalf("the credential was written through a link: %q", data)
	}
	if st, err := os.Lstat(filepath.Join(dir, FileName)); err != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Fatal("agent.env is still a link")
	}
}

func TestCredentialPrefersEnvThenFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, good()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(CredentialVar, "")
	if c, src := Credential(dir); c != "oc_agent_abc_DEF123" || src != SourceFile {
		t.Fatalf("file: got %q %q", c, src)
	}
	t.Setenv(CredentialVar, "oc_agent_fromenv")
	if c, src := Credential(dir); c != "oc_agent_fromenv" || src != SourceEnv {
		t.Fatalf("env: got %q %q", c, src)
	}
	t.Setenv(CredentialVar, "not-a-credential")
	if c, src := Credential(dir); c != "oc_agent_abc_DEF123" || src != SourceFile {
		t.Fatalf("bad env should fall back to file: got %q %q", c, src)
	}
	if c, src := Credential(t.TempDir()); c != "" || src != SourceNone {
		t.Fatalf("empty dir: got %q %q", c, src)
	}
}

func TestDir(t *testing.T) {
	cases := []struct {
		name, home string
		ok         bool
	}{
		{"absolute", "/opt/onecli", true},
		{"relative", "rel/dir", false},
		{"quote", "/opt/one'cli", false},
		{"space", "/opt/one cli", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(HomeVar, tc.home)
			d, err := Dir()
			if (err == nil) != tc.ok {
				t.Fatalf("Dir() = %q, %v; ok want %v", d, err, tc.ok)
			}
		})
	}
}

func TestProfileLineOnceAndRemoved(t *testing.T) {
	profile := filepath.Join(t.TempDir(), ".profile")
	if err := os.WriteFile(profile, []byte("export A=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := "/x/agent.env"
	for i := range 2 {
		changed, err := EnsureProfileLine(profile, env)
		if err != nil {
			t.Fatal(err)
		}
		if changed != (i == 0) {
			t.Fatalf("round %d changed=%v", i, changed)
		}
	}
	data, _ := os.ReadFile(profile)
	if strings.Count(string(data), ProfileLine(env)) != 1 || !strings.HasPrefix(string(data), "export A=1\n") {
		t.Fatalf("profile = %q", data)
	}
	changed, err := RemoveProfileLine(profile, env)
	if err != nil || !changed {
		t.Fatalf("remove: %v %v", changed, err)
	}
	data, _ = os.ReadFile(profile)
	if strings.Contains(string(data), env) || !strings.Contains(string(data), "export A=1") {
		t.Fatalf("profile after remove = %q", data)
	}
}

func TestValidTokens(t *testing.T) {
	cases := map[string]bool{
		"oc_connect_abc_123": true,
		"oc_connect_":        false,
		"oc_connect_a-b":     false,
		"oc_agent_abc":       false,
		"":                   false,
	}
	for in, want := range cases {
		if got := ValidConnectToken(in); got != want {
			t.Errorf("ValidConnectToken(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestWriteMakesANewDirPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if _, err := Write(dir, good()); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("new dir mode = %04o, want 0700", st.Mode().Perm())
	}
}

func TestWriteRefusesAGroupOrWorldWritableDir(t *testing.T) {
	for _, mode := range []os.FileMode{0o770, 0o707, 0o777, os.ModeSticky | 0o777} {
		dir := filepath.Join(t.TempDir(), "shared")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := Write(dir, good()); err == nil || !strings.Contains(err.Error(), "writable by group or others") {
			t.Fatalf("mode %04o: err = %v, want a refusal", mode, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, FileName)); !os.IsNotExist(err) {
			t.Fatalf("mode %04o: agent.env was written", mode)
		}
		st, _ := os.Stat(dir)
		if st.Mode()&(os.ModePerm|os.ModeSticky) != mode {
			t.Fatalf("mode %04o: the dir's mode was changed to %v", mode, st.Mode())
		}
	}
}

func TestWriteAcceptsAnExistingPrivateDir(t *testing.T) {
	for _, mode := range []os.FileMode{0o700, 0o755, 0o750} {
		dir := filepath.Join(t.TempDir(), "mine")
		if err := os.Mkdir(dir, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := Write(dir, good()); err != nil {
			t.Fatalf("mode %04o: %v", mode, err)
		}
	}
}

func TestRemoveProfileLineReplacesALinkAndKeepsTheMode(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere")
	env := "/x/agent.env"
	original := "export A=1\n" + ProfileLine(env) + "\n"
	if err := os.WriteFile(target, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(home, ".profile")
	if err := os.Symlink(target, profile); err != nil {
		t.Skip("symlinks not available")
	}
	changed, err := RemoveProfileLine(profile, env)
	if err != nil || !changed {
		t.Fatalf("remove: %v %v", changed, err)
	}
	if data, _ := os.ReadFile(target); string(data) != original {
		t.Fatalf("the link target was written through: %q", data)
	}
	st, err := os.Lstat(profile)
	if err != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Fatal("profile is still a link")
	}
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("profile mode = %04o, want the original 0640", st.Mode().Perm())
	}
	if data, _ := os.ReadFile(profile); strings.Contains(string(data), env) {
		t.Fatalf("line not removed: %q", data)
	}
}

func TestPrivateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "d")
	if PrivateDir(dir) {
		t.Fatal("a missing dir counted as private")
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if !PrivateDir(dir) {
		t.Fatal("a 0700 dir did not count as private")
	}
	for _, mode := range []os.FileMode{0o720, 0o702, 0o777} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		if PrivateDir(dir) {
			t.Fatalf("mode %04o counted as private", mode)
		}
	}
}
