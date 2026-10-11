//go:build darwin

package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// macOS keeps the session the way Linux does: a 32-byte wrapping key in the
// Keychain and the session in an AES-GCM file beside it. Only /usr/bin/security
// touches the Keychain. Apple signs it, so items it creates open silently for
// every build of this tool. The previous cgo store trusted the binary by code
// hash, so each rebuild met a typed-password dialog, and a session with
// exported E2EE keys is too large for one `security -i` line (4,096 chars).

const (
	legacyService = "io.github.kongesque.line-cli"
	legacyAccount = "default"
)

var errRealStoreInTest = errors.New("tests must not use the real Keychain or session file; set LINE_CLI_LIVE_READ=1 for a live test")

func darwinStore() (secretFileStore, error) {
	if testing.Testing() && os.Getenv("LINE_CLI_LIVE_READ") != "1" {
		return secretFileStore{}, errRealStoreInTest
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return secretFileStore{}, err
	}
	dir, err = filepath.Abs(filepath.Join(dir, "line-cli"))
	if err != nil {
		return secretFileStore{}, err
	}
	return secretFileStore{path: filepath.Join(dir, "session.enc"), secrets: secretToolStore{run: runSecurityTool}}, nil
}

// legacyKeychain reaches the item the cgo build wrote. Reading it asks for the
// login password once, because that item trusts old binaries by hash; it is
// read only when no new session exists, then deleted, which never prompts.
type legacyKeychain struct {
	exec func(args []string, stdin []byte, timeout time.Duration) securityResult
}

var legacy = legacyKeychain{exec: execSecurity}

func (l legacyKeychain) read() ([]byte, error) {
	r := l.exec([]string{"find-generic-password", "-s", legacyService, "-a", legacyAccount, "-w"}, nil, 2*time.Minute)
	if r.err == nil && r.code == securityNotFound {
		return nil, ErrNotFound
	}
	if r.err != nil || r.code != 0 {
		clear(r.stdout)
		return nil, errors.Join(ErrStorageUnavailable, errors.New("could not read the previous Keychain session; run line login"))
	}
	value, err := decodeSecurityPassword(r.stdout)
	clear(r.stdout)
	return value, err
}

func (l legacyKeychain) remove() error {
	r := l.exec([]string{"delete-generic-password", "-s", legacyService, "-a", legacyAccount}, nil, 30*time.Second)
	if r.err == nil && (r.code == 0 || r.code == securityNotFound) {
		return nil
	}
	return errors.Join(ErrStorageUnavailable, errors.New("could not remove the previous Keychain session"))
}

// migrate moves a session saved by the cgo build into the new store. It runs
// only when the new store has nothing, so it happens once per machine.
func (l legacyKeychain) migrate(store Store) (*State, error) {
	data, err := l.read()
	if err != nil {
		return nil, err
	}
	defer clear(data)
	state, err := decodeSession(data)
	if err != nil {
		return nil, err
	}
	if err := store.Save(state); err != nil {
		return nil, err
	}
	if err := l.remove(); err != nil {
		return nil, err
	}
	return state, nil
}

func (KeychainStore) Load() (*State, error) {
	store, err := darwinStore()
	if err != nil {
		return nil, err
	}
	state, err := store.Load()
	if errors.Is(err, ErrNotFound) {
		return legacy.migrate(store)
	}
	return state, err
}

func (KeychainStore) Save(state *State) error {
	store, err := darwinStore()
	if err != nil {
		return err
	}
	if err := store.Save(state); err != nil {
		return err
	}
	// A watcher still on the cgo build may write the old item back; keep
	// removing it so nothing reads a stale copy.
	return legacy.remove()
}

func (KeychainStore) Delete() error {
	store, err := darwinStore()
	if err != nil {
		return err
	}
	return errors.Join(store.Delete(), legacy.remove())
}

func prepareNativeStorage() error {
	store, err := darwinStore()
	if err != nil {
		return err
	}
	return prepareSecretFileStore(store)
}

// Status reads only the new store, so it can never raise the legacy item's
// password dialog.
func platformStorageStatus(check bool) (StorageStatus, error) {
	store, err := darwinStore()
	if err != nil {
		return initialStorageStatus("native"), err
	}
	return nativeStorageStatus(store, check, func() error { return prepareSecretFileStore(store) })
}
