package piweb

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	adminPasswordHashKey = "adminPasswordHash"
	adminPasswordHeader  = "X-Admin-Password"
	adminPasswordScheme  = "pbkdf2-sha256"
	adminPasswordRounds  = 120000
	adminPasswordSaltLen = 16
	adminPasswordKeyLen  = 32
	adminPasswordMinLen  = 8
	adminPasswordMaxLen  = 128
)

var (
	ErrAdminPasswordAlreadyConfigured = errors.New("admin password is already configured")
	ErrAdminPasswordInvalid           = errors.New("admin password is invalid")
	ErrAdminPasswordRequired          = errors.New("admin password is required")
	ErrAdminPasswordValidation        = errors.New("admin password validation error")
)

type AdminPasswordStatusResponse struct {
	Configured bool `json:"configured"`
}

type AdminPasswordSetupRequest struct {
	Password     string `json:"password"`
	Confirmation string `json:"confirmation"`
}

func AdminPasswordConfigured() (bool, error) {
	appConfigMu.Lock()
	defer appConfigMu.Unlock()

	stored, err := readStoredAppConfigLocked()
	if err != nil {
		return false, err
	}
	hash, ok := stored[adminPasswordHashKey].(string)
	return ok && hash != "", nil
}

func SetupAdminPassword(password string) error {
	if err := validateAdminPassword(password); err != nil {
		return err
	}

	appConfigMu.Lock()
	defer appConfigMu.Unlock()

	path, stored, err := loadStoredAppConfigLocked()
	if err != nil {
		return err
	}
	if configured, _ := stored[adminPasswordHashKey].(string); configured != "" {
		return ErrAdminPasswordAlreadyConfigured
	}
	hash, err := hashAdminPassword(password)
	if err != nil {
		return err
	}
	stored[adminPasswordHashKey] = hash
	if _, ok := stored["version"]; !ok {
		stored["version"] = float64(1)
	}
	return writeAppConfig(path, stored)
}

func VerifyAdminPassword(password string) error {
	appConfigMu.Lock()
	defer appConfigMu.Unlock()

	stored, err := readStoredAppConfigLocked()
	if err != nil {
		return err
	}
	hash, ok := stored[adminPasswordHashKey].(string)
	if !ok || hash == "" {
		return ErrAdminPasswordRequired
	}
	if !verifyAdminPasswordHash(password, hash) {
		return ErrAdminPasswordInvalid
	}
	return nil
}

func validateAdminPassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < adminPasswordMinLen || length > adminPasswordMaxLen {
		return fmt.Errorf("%w: password must contain %d-%d characters", ErrAdminPasswordValidation, adminPasswordMinLen, adminPasswordMaxLen)
	}
	return nil
}

func hashAdminPassword(password string) (string, error) {
	salt := make([]byte, adminPasswordSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate admin password salt: %w", err)
	}
	key := deriveAdminPasswordKey(password, salt, adminPasswordRounds, adminPasswordKeyLen)
	return strings.Join([]string{
		adminPasswordScheme,
		strconv.Itoa(adminPasswordRounds),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	}, "$"), nil
}

func verifyAdminPasswordHash(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != adminPasswordScheme {
		return false
	}
	rounds, err := strconv.Atoi(parts[1])
	if err != nil || rounds < 10000 || rounds > 1000000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) != adminPasswordKeyLen {
		return false
	}
	actual := deriveAdminPasswordKey(password, salt, rounds, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func deriveAdminPasswordKey(password string, salt []byte, rounds, keyLen int) []byte {
	result := make([]byte, 0, keyLen)
	for block := 1; len(result) < keyLen; block++ {
		mac := hmac.New(sha256.New, []byte(password))
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		previous := mac.Sum(nil)
		blockResult := append([]byte(nil), previous...)
		for round := 1; round < rounds; round++ {
			mac = hmac.New(sha256.New, []byte(password))
			mac.Write(previous)
			previous = mac.Sum(nil)
			for index := range blockResult {
				blockResult[index] ^= previous[index]
			}
		}
		result = append(result, blockResult...)
	}
	return result[:keyLen]
}

func readStoredAppConfigLocked() (map[string]any, error) {
	path, err := AppConfigPath()
	if err != nil {
		return nil, err
	}
	return readAppConfig(path)
}

func loadStoredAppConfigLocked() (string, map[string]any, error) {
	path, err := AppConfigPath()
	if err != nil {
		return "", nil, err
	}
	stored, err := readAppConfig(path)
	if err != nil {
		return "", nil, err
	}
	return path, stored, nil
}
