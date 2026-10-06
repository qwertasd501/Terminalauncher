package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	env "github.com/qwertasd501/Terminalauncher/pkg"
)

// Store is the global authentication store.
var Store AuthStore

type msaAuthStore struct {
	AccessToken  string    `json:"access_token"`
	Expires      time.Time `json:"expires"`
	RefreshToken string    `json:"refresh_token"`
}

func (store *msaAuthStore) isValid() bool {
	return store.AccessToken != "" && store.Expires.After(time.Now())
}
func (store *msaAuthStore) write(resp msaResponse) {
	store.AccessToken = resp.AccessToken
	store.Expires = time.Now().Add(time.Second * time.Duration(resp.ExpiresIn))
	store.RefreshToken = resp.RefreshToken
}

type xblAuthStore struct {
	Userhash string    `json:"uhs"`
	Token    string    `json:"token"`
	Expires  time.Time `json:"expires"`
}

func (store *xblAuthStore) isValid() bool {
	return store.Token != "" && store.Userhash != "" && store.Expires.After(time.Now())
}
func (store *xblAuthStore) write(resp xblResponse) {
	// Guard against an empty claim list, which would otherwise panic.
	if len(resp.DisplayClaims.Xui) > 0 {
		store.Userhash = resp.DisplayClaims.Xui[0].Uhs
	}
	store.Token = resp.Token
	store.Expires = resp.NotAfter
}

type xstsAuthStore struct {
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}

func (store *xstsAuthStore) isValid() bool {
	return store.Token != "" && store.Expires.After(time.Now())
}
func (store *xstsAuthStore) write(resp xstsResponse) {
	store.Token = resp.Token
	store.Expires = resp.NotAfter
}

type minecraftAuthStore struct {
	AccessToken string    `json:"access_token"`
	Expires     time.Time `json:"expires"`
	Username    string    `json:"name"`
	UUID        string    `json:"id"`
}

func (store *minecraftAuthStore) isValid() bool {
	return store.AccessToken != "" && store.Expires.After(time.Now())
}
func (store *minecraftAuthStore) write(resp minecraftResponse, profile minecraftProfile) {
	store.AccessToken = resp.AccessToken
	store.Expires = time.Now().Add(time.Second * time.Duration(resp.ExpiresIn))
	store.Username = profile.Name
	store.UUID = profile.ID
}

// Account types.
const (
	AccountMicrosoft = "microsoft"
	AccountOffline   = "offline"
)

// An Account is one entry in the account list: either a Microsoft account, holding every token
// needed to sign in, or a local username that launches the game without signing in at all.
type Account struct {
	// Type is "microsoft" or "offline". An empty value counts as "microsoft", so stores written
	// before offline accounts existed keep working.
	Type string `json:"type,omitempty"`
	// Username is the player name of a local account. Microsoft accounts take their name from
	// Minecraft.Username instead.
	Username string `json:"username,omitempty"`

	MSA       msaAuthStore       `json:"msa"`
	XBL       xblAuthStore       `json:"xbl"`
	XSTS      xstsAuthStore      `json:"xsts"`
	Minecraft minecraftAuthStore `json:"minecraft"`
}

// IsOffline reports whether the account launches the game without signing in.
func (account *Account) IsOffline() bool {
	return account.Type == AccountOffline
}

// Kind returns the account type, defaulting to Microsoft when it was not recorded.
func (account *Account) Kind() string {
	if account.Type == "" {
		return AccountMicrosoft
	}
	return account.Type
}

// Label is the name an account is listed under.
func (account *Account) Label() string {
	if account.IsOffline() {
		if account.Username != "" {
			return account.Username
		}
		return "offline"
	}
	if account.Minecraft.Username != "" {
		return account.Minecraft.Username
	}
	if account.Minecraft.UUID != "" {
		return account.Minecraft.UUID
	}
	return "unauthenticated"
}

// OfflineSession returns the session used to launch the game as this account.
func (account *Account) OfflineSession() Session {
	return Session{Username: account.Username}
}

// ValidateUsername checks a local player name against Minecraft's own rules.
func ValidateUsername(username string) error {
	if len(username) < 3 || len(username) > 16 {
		return fmt.Errorf("username must be 3 to 16 characters long")
	}
	for _, r := range username {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
		if !allowed {
			return fmt.Errorf("username may only contain letters, digits and underscores")
		}
	}
	return nil
}

// An AuthStore holds all known accounts and which one is active.
type AuthStore struct {
	Accounts []*Account `json:"accounts"`
	Current  int        `json:"current"`
}

// WriteToCache writes the store to the cache file.
func (store *AuthStore) WriteToCache() error {
	data, _ := json.MarshalIndent(store, "", "    ")
	return os.WriteFile(env.AuthStorePath, data, 0644)
}

// Clear removes every account and writes the store to the cache file.
func (store *AuthStore) Clear() error {
	*store = AuthStore{Current: -1}
	return store.WriteToCache()
}

// Active returns the currently selected account, or nil if there is none.
func (store *AuthStore) Active() *Account {
	if store.Current < 0 || store.Current >= len(store.Accounts) {
		return nil
	}
	return store.Accounts[store.Current]
}

// Add appends a new empty account, makes it current, and returns it.
func (store *AuthStore) Add() *Account {
	account := &Account{}
	store.Accounts = append(store.Accounts, account)
	store.Current = len(store.Accounts) - 1
	return account
}

// SetCurrent makes the account at index active.
func (store *AuthStore) SetCurrent(index int) error {
	if index < 0 || index >= len(store.Accounts) {
		return fmt.Errorf("no such account")
	}
	store.Current = index
	return nil
}

// AddOffline appends a local username account, makes it current, and returns it.
//
// Offline accounts launch the game without signing in, which is what a player without a Microsoft
// account needs.
func (store *AuthStore) AddOffline(username string) (*Account, error) {
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	for _, account := range store.Accounts {
		if account.IsOffline() && strings.EqualFold(account.Username, username) {
			return nil, fmt.Errorf("offline account %q already exists", username)
		}
	}

	account := &Account{Type: AccountOffline, Username: username}
	store.Accounts = append(store.Accounts, account)
	store.Current = len(store.Accounts) - 1
	return account, nil
}

// Remove deletes the account at index and adjusts the selection.
func (store *AuthStore) Remove(index int) error {
	if index < 0 || index >= len(store.Accounts) {
		return fmt.Errorf("no such account")
	}
	store.Accounts = append(store.Accounts[:index], store.Accounts[index+1:]...)
	switch {
	case len(store.Accounts) == 0:
		store.Current = -1
	case store.Current > index:
		store.Current--
	case store.Current >= len(store.Accounts):
		store.Current = len(store.Accounts) - 1
	}
	return nil
}

// Dedupe drops duplicate accounts, keeping the last entry for each UUID.
//
// A fresh login always appends an account, so re-authenticating an account that is already known
// would otherwise leave two entries behind.
func (store *AuthStore) Dedupe() {
	last := make(map[string]int)
	for i, account := range store.Accounts {
		key := account.Minecraft.UUID
		if key == "" {
			key = fmt.Sprintf("anonymous-%d", i)
		}
		last[key] = i
	}

	kept := make([]*Account, 0, len(store.Accounts))
	current := -1
	for i, account := range store.Accounts {
		key := account.Minecraft.UUID
		if key == "" {
			key = fmt.Sprintf("anonymous-%d", i)
		}
		if last[key] != i {
			continue
		}
		if i == store.Current {
			current = len(kept)
		}
		kept = append(kept, account)
	}
	store.Accounts = kept
	if current == -1 {
		current = len(kept) - 1
	}
	store.Current = current
}

// legacyAuthStore is the single-account layout used before accounts became a list.
type legacyAuthStore struct {
	MSA       msaAuthStore       `json:"msa"`
	XBL       xblAuthStore       `json:"xbl"`
	XSTS      xstsAuthStore      `json:"xsts"`
	Minecraft minecraftAuthStore `json:"minecraft"`
}

// decodeStore parses the store, migrating the legacy single-account layout when it is found.
func decodeStore(data []byte) AuthStore {
	var store AuthStore
	if err := json.Unmarshal(data, &store); err == nil && store.Accounts != nil {
		if store.Current < 0 || store.Current >= len(store.Accounts) {
			store.Current = len(store.Accounts) - 1
		}
		return store
	}

	var legacy legacyAuthStore
	if err := json.Unmarshal(data, &legacy); err == nil && legacy.MSA.RefreshToken != "" {
		return AuthStore{
			Accounts: []*Account{{
				MSA:       legacy.MSA,
				XBL:       legacy.XBL,
				XSTS:      legacy.XSTS,
				Minecraft: legacy.Minecraft,
			}},
			Current: 0,
		}
	}

	return AuthStore{Current: -1}
}

// ReadFromCache reads an AuthStore into the global store from the cache file.
//
// This function should be run in order to load the authentication info from the cache. If it is not, the global AuthStore will be blank.
func ReadFromCache() error {
	cache, err := os.ReadFile(env.AuthStorePath)
	if errors.Is(err, os.ErrNotExist) {
		cache, err = migrateLegacyStore()
		if err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("read auth store: %w", err)
	}

	Store = decodeStore(cache)
	return nil
}

// migrateLegacyStore reads a game-directory-scoped account store, if one is left over from an
// earlier version of the launcher, and writes it to the new global location.
func migrateLegacyStore() ([]byte, error) {
	legacy, err := os.ReadFile(env.LegacyAuthStorePath)
	if err == nil {
		store := decodeStore(legacy)
		if len(store.Accounts) > 0 {
			Store = store
			if err := Store.WriteToCache(); err != nil {
				return nil, fmt.Errorf("migrate auth store: %w", err)
			}
			return legacy, nil
		}
	}

	if err := os.MkdirAll(env.ConfigDir, 0755); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}
	if _, err := os.Create(env.AuthStorePath); err != nil {
		return nil, fmt.Errorf("create auth store: %w", err)
	}
	return []byte{}, nil
}
