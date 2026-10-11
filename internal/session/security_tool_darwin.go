//go:build darwin

package session

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// securityNotFound is /usr/bin/security's exit status for a missing item
// (errSecItemNotFound, -25300, truncated to 44).
const securityNotFound = 44

type securityResult struct {
	stdout []byte
	code   int
	err    error
}

func execSecurity(args []string, stdin []byte, timeout time.Duration) securityResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	clear(stderr.Bytes())
	r := securityResult{stdout: stdout.Bytes(), err: err}
	var exit *exec.ExitError
	if errors.As(err, &exit) && ctx.Err() == nil {
		r.code, r.err = exit.ExitCode(), nil
	}
	if stdout.Len() > maxSessionBytes {
		clear(r.stdout)
		r.stdout, r.err = nil, errHelperOutputLimit
	}
	return r
}

// decodeSecurityPassword undoes `find-generic-password -w`, which prints a
// printable value as is and anything else as bare hex.
func decodeSecurityPassword(out []byte) ([]byte, error) {
	value := bytes.TrimSuffix(out, []byte("\n"))
	if len(value) > 0 && len(value)%2 == 0 && isHex(value) {
		decoded := make([]byte, hex.DecodedLen(len(value)))
		if _, err := hex.Decode(decoded, value); err == nil {
			return decoded, nil
		}
	}
	return append([]byte(nil), value...), nil
}

func isHex(b []byte) bool {
	for _, c := range b {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

// securityTool answers secretToolStore's secret-tool calls
// (lookup | store --label=L | clear, then service S account A) with
// /usr/bin/security, so the Linux file store runs unchanged on macOS.
type securityTool struct {
	exec func(args []string, stdin []byte, timeout time.Duration) securityResult
}

func runSecurityTool(args []string, input []byte) secretResult {
	return securityTool{exec: execSecurity}.run(args, input)
}

func (t securityTool) run(args []string, input []byte) secretResult {
	action, label, service, account, err := parseSecretToolArgs(args)
	if err != nil {
		return secretResult{err: err}
	}
	switch action {
	case "lookup":
		r := t.exec([]string{"find-generic-password", "-s", service, "-a", account, "-w"}, nil, 30*time.Second)
		if r.err == nil && r.code == securityNotFound {
			return secretResult{code: 1}
		}
		if r.err != nil || r.code != 0 {
			clear(r.stdout)
			return secretResult{code: r.code, diagnostic: true, err: r.err}
		}
		value, _ := decodeSecurityPassword(r.stdout)
		clear(r.stdout)
		return secretResult{output: value}
	case "clear":
		r := t.exec([]string{"delete-generic-password", "-s", service, "-a", account}, nil, 30*time.Second)
		if r.err == nil && r.code == securityNotFound {
			return secretResult{code: 1}
		}
		return secretResult{code: r.code, diagnostic: r.code != 0, err: r.err}
	case "store":
		return t.store(label, service, account, input)
	}
	return secretResult{err: fmt.Errorf("unsupported credential action %q", action)}
}

// store re-creates the item rather than updating it, so security owns it, and
// passes the value as hex on stdin through `security -i`, never on argv.
func (t securityTool) store(label, service, account string, value []byte) secretResult {
	if len(value) == 0 || len(value) > 1500 {
		return secretResult{err: errors.New("credential value must be 1 to 1500 bytes for security -i")}
	}
	del := t.exec([]string{"delete-generic-password", "-s", service, "-a", account}, nil, 30*time.Second)
	if del.err != nil || (del.code != 0 && del.code != securityNotFound) {
		return secretResult{code: del.code, diagnostic: true, err: del.err}
	}
	line := fmt.Sprintf("add-generic-password -s '%s' -a '%s' -l '%s' -X %s\n", service, account, label, hex.EncodeToString(value))
	cmd := []byte(line)
	defer clear(cmd)
	add := t.exec([]string{"-i"}, cmd, 30*time.Second)
	clear(add.stdout)
	if add.err != nil || add.code != 0 {
		return secretResult{code: add.code, diagnostic: true, err: add.err}
	}
	// `security -i` exits 0 even when a command inside it fails; read it back.
	check := t.run([]string{"lookup", "service", service, "account", account}, nil)
	defer clear(check.output)
	if check.err != nil || check.code != 0 || !bytes.Equal(check.output, value) {
		return secretResult{code: 1, diagnostic: true, err: errors.New("credential could not be verified after saving")}
	}
	return secretResult{}
}

func parseSecretToolArgs(args []string) (action, label, service, account string, err error) {
	if len(args) == 0 {
		return "", "", "", "", errors.New("missing credential action")
	}
	action = args[0]
	rest := args[1:]
	if len(rest) > 0 && strings.HasPrefix(rest[0], "--label=") {
		label, rest = strings.TrimPrefix(rest[0], "--label="), rest[1:]
	}
	if len(rest) != 4 || rest[0] != "service" || rest[2] != "account" {
		return "", "", "", "", errors.New("unexpected credential arguments")
	}
	service, account = rest[1], rest[3]
	for _, v := range []string{label, service, account} {
		if strings.ContainsAny(v, "'\"\\\n\r") {
			return "", "", "", "", errors.New("credential names cannot contain quotes, backslashes or newlines")
		}
	}
	return action, label, service, account, nil
}
