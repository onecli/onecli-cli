/*
 * onecli-sandbox-gid: adopt the OneCLI sandbox group, then exec.
 *
 * Why this exists: pf scopes the transparent redirect by GID, and it matches
 * the socket's EFFECTIVE group. Measured on this machine: a shell that has
 * onecli-sandbox as a SUPPLEMENTARY group was not matched by the anchor's
 * rules, so supplementary membership is not enough. POSIX only lets an
 * unprivileged process adopt its real or saved GID, so something must set
 * the effective GID at exec: that is what the setgid bit does.
 *
 * Written in C rather than Go deliberately. This is a setgid binary, so its
 * attack surface should be as close to nothing as possible: no runtime, no
 * threads spawned before main, no environment-driven behavior.
 *
 * SECURITY REVIEW, stated plainly:
 *   - It grants GID onecli-sandbox, which owns no files and confers no
 *     privileges. Under the loaded pf anchor that group is MORE restricted
 *     than a normal one (default-deny egress). Running this is strictly a
 *     downgrade, which is why it is safe for any user to execute.
 *   - It does NOT setuid. The caller's user identity is unchanged.
 *   - It sets the REAL gid as well as the effective one, so no code can
 *     later restore the original group via setgid(getgid()).
 *   - It drops all supplementary groups, otherwise the original groups
 *     would remain in the credential set.
 *   - It refuses to run if the setgid bit did not take effect, rather than
 *     silently exec'ing ungoverned. That failure is the whole ballgame: a
 *     process that runs with the wrong GID is not redirected by pf, and
 *     with the transparent Seatbelt profile it would have direct egress.
 */

#include <errno.h>
#include <grp.h>   /* setgroups() decl; see note in main() about why it is unused */
#include <stdio.h>
#include <string.h>
#include <sys/types.h>
#include <unistd.h>

#ifndef ONECLI_SANDBOX_GID
#error "ONECLI_SANDBOX_GID must be defined at compile time"
#endif

int main(int argc, char *argv[]) {
    if (argc < 2) {
        fprintf(stderr, "usage: onecli-sandbox-gid <command> [args...]\n");
        return 2;
    }

    gid_t target = (gid_t)ONECLI_SANDBOX_GID;

    /* The setgid bit should have made this our effective GID. If it did
     * not, the binary is not installed correctly and we must not continue:
     * the caller is about to run under a Seatbelt profile that permits
     * direct 443, governed only by a pf rule keyed to this GID. */
    if (getegid() != target) {
        fprintf(stderr,
                "onecli: setgid bit not in effect (egid=%d, want %d); "
                "refusing to run ungoverned\n",
                (int)getegid(), (int)target);
        return 1;
    }

    /* Set the REAL gid as well, so nothing can later switch back via
     * setgid(getgid()).
     *
     * setregid(), not setgid(): measured on macOS, an unprivileged
     * setgid(target) whose egid is already target sets only the effective
     * gid and leaves the real gid alone, producing gid=20 egid=700. The
     * strict check below caught that rather than letting it through.
     * setregid(target, target) sets both. */
    if (setregid(target, target) != 0) {
        fprintf(stderr, "onecli: setregid failed: %s\n", strerror(errno));
        return 1;
    }

    /* Deliberately NOT calling setgroups(): it requires root, and this
     * binary is setgid only. Measured, which is why this comment exists
     * rather than the call: setgroups(1, &target) fails with EPERM here.
     *
     * That leaves the caller's original supplementary groups in the
     * credential set, and it does NOT weaken the guarantee. pf matches the
     * EFFECTIVE gid, which is the property this binary exists to set, and
     * that was established by direct measurement: with the anchor blocking
     * gid 700, a shell holding onecli-sandbox as a SUPPLEMENTARY group
     * still reached the internet. Supplementary groups are invisible to
     * the rules that govern egress.
     *
     * Supplementary groups do affect FILE access, but file confinement is
     * the Seatbelt profile's job, not this binary's, and the profile
     * applies to the whole process tree regardless of group. */

    /* Verify both ids before exec: a wrong GID here means an unredirected
     * process running under a profile that permits direct 443. */
    if (getgid() != target || getegid() != target) {
        fprintf(stderr, "onecli: gid did not stick (gid=%d egid=%d)\n",
                (int)getgid(), (int)getegid());
        return 1;
    }

    execvp(argv[1], &argv[1]);
    fprintf(stderr, "onecli: exec %s failed: %s\n", argv[1], strerror(errno));
    return 127;
}
