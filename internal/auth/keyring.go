package auth

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// EnvNoKeyring forces the file backend, skipping the OS keyring entirely.
const EnvNoKeyring = "NPMCTL_NO_KEYRING"

// keyringService namespaces npmctl's entries in the OS credential store.
const keyringService = "npmctl"

// KeyringStore uses the platform keyring: macOS Keychain, Windows Credential
// Manager, or Secret Service on Linux.
type KeyringStore struct{}

func (KeyringStore) Backend() string { return "os-keyring" }

var (
	availableOnce sync.Once
	availableVal  bool
)

// Available probes the keyring rather than guessing from the platform: Secret
// Service needs a D-Bus session, which a headless Linux box lacks even though
// the library compiles there fine.
//
// The probe is a READ. An earlier version wrote and deleted a throwaway item,
// which meant every npmctl invocation mutated the user's keychain — and on macOS
// that can block on an authorisation prompt. A lookup that comes back
// "not found" proves the backend is reachable just as well.
//
// The result is cached: the answer cannot change during one invocation, and
// probing once per call would add latency to every command.
func (k KeyringStore) Available() bool {
	availableOnce.Do(func() {
		if strings.TrimSpace(os.Getenv(EnvNoKeyring)) == "1" {
			availableVal = false
			return
		}
		_, err := keyring.Get(keyringService, "npmctl-availability-probe")
		availableVal = err == nil || errors.Is(err, keyring.ErrNotFound)
	})
	return availableVal
}

func (k KeyringStore) Load(profile, url, identity string) (*Credential, error) {
	key := Key(profile, url, identity)
	raw, err := keyring.Get(keyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, &ErrNotFound{Key: key}
	}
	if err != nil {
		return nil, err
	}
	var c Credential
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (k KeyringStore) Save(c *Credential) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, c.Key(), string(b))
}

func (k KeyringStore) Delete(profile, url, identity string) error {
	err := keyring.Delete(keyringService, Key(profile, url, identity))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
