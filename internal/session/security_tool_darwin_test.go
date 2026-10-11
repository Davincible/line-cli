//go:build darwin

package session

import (
	"bytes"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeSecurity is an in-memory stand-in for /usr/bin/security. It parses the
// exact command lines the store sends, so the tests check the real wire shape
// without touching the Keychain.
type fakeSecurity struct {
	items    map[string][]byte // service/account -> value
	calls    [][]string
	stdin    []string
	readFail bool
}

var addLine = regexp.MustCompile(`^add-generic-password -s '([^']*)' -a '([^']*)' -l '([^']*)' -X ([0-9a-f]+)\n$`)

func (f *fakeSecurity) exec(args []string, stdin []byte, _ time.Duration) securityResult {
	f.calls = append(f.calls, append([]string(nil), args...))
	key := func(a []string) string { return a[2] + "/" + a[4] }
	switch args[0] {
	case "find-generic-password":
		if f.readFail {
			return securityResult{code: 51}
		}
		v, ok := f.items[key(args)]
		if !ok {
			return securityResult{code: securityNotFound}
		}
		if bytes.IndexFunc(v, func(r rune) bool { return r < 0x20 || r > 0x7e }) >= 0 {
			return securityResult{stdout: []byte(hex.EncodeToString(v) + "\n")}
		}
		return securityResult{stdout: append(append([]byte(nil), v...), '\n')}
	case "delete-generic-password":
		if _, ok := f.items[key(args)]; !ok {
			return securityResult{code: securityNotFound}
		}
		delete(f.items, key(args))
		return securityResult{}
	case "-i":
		f.stdin = append(f.stdin, string(stdin))
		m := addLine.FindStringSubmatch(string(stdin))
		if m == nil {
			return securityResult{} // security -i exits 0 on a bad command
		}
		v, _ := hex.DecodeString(m[4])
		f.items[m[1]+"/"+m[2]] = v
		return securityResult{}
	}
	return securityResult{err: errors.New("unexpected security call")}
}

func newFakeSecurity() *fakeSecurity { return &fakeSecurity{items: map[string][]byte{}} }

func fakeFileStore(t *testing.T, f *fakeSecurity) secretFileStore {
	tool := securityTool{exec: f.exec}
	return secretFileStore{path: filepath.Join(t.TempDir(), "line-cli", "session.enc"), secrets: secretToolStore{run: tool.run}}
}

func TestSecurityStoreRoundTripKeepsValueOffArgv(t *testing.T) {
	f := newFakeSecurity()
	store := fakeFileStore(t, f)
	state := &State{Version: 1, MID: "u1", AccessToken: "token", ExportedKeys: map[string]string{"k": strings.Repeat("x", 20000)}}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || got.AccessToken != "token" || len(got.ExportedKeys["k"]) != 20000 {
		t.Fatalf("round trip failed: %v %+v", err, got)
	}
	key := f.items["io.github.kongesque.line-cli.encryption/default"]
	if len(key) != 44 {
		t.Fatalf("expected a base64 32-byte wrapping key in the Keychain, got %d bytes", len(key))
	}
	for _, call := range f.calls {
		for _, a := range call {
			if a == string(key) || strings.Contains(a, hex.EncodeToString(key)) {
				t.Fatalf("wrapping key appeared on argv: %v", call)
			}
		}
	}
	if len(f.stdin) != 1 || !strings.HasPrefix(f.stdin[0], "add-generic-password -s 'io.github.kongesque.line-cli.encryption' -a 'default' -l 'LINE CLI session' -X ") {
		t.Fatalf("unexpected security -i input: %q", f.stdin)
	}
}

func TestSecurityStoreRecreatesExistingItem(t *testing.T) {
	f := newFakeSecurity()
	f.items["svc/acct"] = []byte("old")
	tool := securityTool{exec: f.exec}
	if r := tool.run([]string{"store", "--label=L", "service", "svc", "account", "acct"}, []byte("new")); r.err != nil || r.code != 0 {
		t.Fatalf("store failed: %+v", r)
	}
	if f.calls[0][0] != "delete-generic-password" || string(f.items["svc/acct"]) != "new" {
		t.Fatalf("item was not deleted then re-created: %v", f.calls)
	}
}

func TestSecurityStoreFailsWhenWriteDoesNotLand(t *testing.T) {
	f := newFakeSecurity()
	tool := securityTool{exec: func(args []string, stdin []byte, d time.Duration) securityResult {
		if args[0] == "-i" {
			return securityResult{} // exit 0, nothing written
		}
		return f.exec(args, stdin, d)
	}}
	if r := tool.run([]string{"store", "--label=L", "service", "svc", "account", "acct"}, []byte("v")); r.err == nil {
		t.Fatal("a write that did not land must fail")
	}
}

func TestSecurityToolLookupAndClearMapExitCodes(t *testing.T) {
	f := newFakeSecurity()
	tool := securityTool{exec: f.exec}
	if r := tool.run([]string{"lookup", "service", "s", "account", "a"}, nil); r.code != 1 || r.diagnostic || r.err != nil {
		t.Fatalf("missing item must read as not found: %+v", r)
	}
	if r := tool.run([]string{"clear", "service", "s", "account", "a"}, nil); r.code != 1 || r.diagnostic {
		t.Fatalf("clearing a missing item must be benign: %+v", r)
	}
	f.readFail = true
	if r := tool.run([]string{"lookup", "service", "s", "account", "a"}, nil); r.code == 1 && !r.diagnostic {
		t.Fatalf("a failed read must not look like a missing item: %+v", r)
	}
}

func TestSecurityToolRejectsQuotesInNames(t *testing.T) {
	tool := securityTool{exec: newFakeSecurity().exec}
	if r := tool.run([]string{"store", "--label=L", "service", "s'; rm", "account", "a"}, []byte("v")); r.err == nil {
		t.Fatal("quotes in a name must be refused")
	}
}

func TestDecodeSecurityPasswordHandlesHex(t *testing.T) {
	got, _ := decodeSecurityPassword([]byte(hex.EncodeToString([]byte("a\x00b")) + "\n"))
	if string(got) != "a\x00b" {
		t.Fatalf("hex form not decoded: %q", got)
	}
	got, _ = decodeSecurityPassword([]byte(`{"version":1}` + "\n"))
	if string(got) != `{"version":1}` {
		t.Fatalf("plain form changed: %q", got)
	}
}

func TestLegacyMigrationMovesSessionOnceAndDeletesOldItem(t *testing.T) {
	f := newFakeSecurity()
	f.items[legacyService+"/"+legacyAccount] = []byte(`{"version":1,"mid":"u1","access_token":"tok"}`)
	l := legacyKeychain{exec: f.exec}
	store := fakeFileStore(t, f)
	state, err := l.migrate(store)
	if err != nil || state.AccessToken != "tok" {
		t.Fatalf("migration failed: %v %+v", err, state)
	}
	if _, ok := f.items[legacyService+"/"+legacyAccount]; ok {
		t.Fatal("legacy item was not deleted")
	}
	if got, err := store.Load(); err != nil || got.AccessToken != "tok" {
		t.Fatalf("migrated session not in the new store: %v", err)
	}
	if _, err := l.migrate(store); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second migration must find nothing: %v", err)
	}
}

func TestLegacyReadFailureIsNotNotFound(t *testing.T) {
	f := newFakeSecurity()
	f.readFail = true
	if _, err := (legacyKeychain{exec: f.exec}).read(); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("a refused legacy read must not look like no session: %v", err)
	}
}

func TestPrepareSecretFileStoreOnSecurity(t *testing.T) {
	f := newFakeSecurity()
	if err := prepareSecretFileStore(fakeFileStore(t, f)); err != nil {
		t.Fatal(err)
	}
	if len(f.items) != 0 {
		t.Fatalf("storage check left items behind: %v", f.items)
	}
}

func TestRealStoreRefusedUnderGoTest(t *testing.T) {
	t.Setenv("LINE_CLI_LIVE_READ", "")
	if _, err := (KeychainStore{}).Load(); !errors.Is(err, errRealStoreInTest) {
		t.Fatalf("the real store must refuse to run under go test: %v", err)
	}
}
