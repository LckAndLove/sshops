package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/pbkdf2"
)

const (
	saltSize   = 16
	keySize    = 32
	pbkdf2Iter = 100000
	nonceSize  = 12
)

type Credential struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	KeyPath  string `json:"key_path"`
	KeyPass  string `json:"key_pass"`
}

type Vault struct {
	path      string
	masterKey []byte
	salt      []byte
	creds     map[string]*Credential
}

func NewVault(path string) *Vault {
	return &Vault{path: path}
}

func (v *Vault) Unlock(masterPassword string) error {
	if v == nil {
		return errors.New("vault 未初始化")
	}

	if masterPassword == "" {
		return errors.New("vault 主密码不能为空")
	}

	data, err := os.ReadFile(v.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		salt := make([]byte, saltSize)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return err
		}
		v.salt = salt
		v.masterKey = deriveKey(masterPassword, salt)
		v.creds = map[string]*Credential{}
		return v.persist()
	}

	if len(data) < saltSize {
		return errors.New("vault 文件损坏")
	}

	salt := make([]byte, saltSize)
	copy(salt, data[:saltSize])
	cipherBlob := data[saltSize:]

	v.salt = salt
	v.masterKey = deriveKey(masterPassword, salt)

	if len(cipherBlob) == 0 {
		v.creds = map[string]*Credential{}
		return nil
	}

	plain, err := decrypt(v.masterKey, cipherBlob)
	if err != nil {
		v.Lock()
		return errors.New("vault 主密码错误或数据已损坏")
	}

	creds := map[string]*Credential{}
	if len(plain) > 0 {
		if err := json.Unmarshal(plain, &creds); err != nil {
			v.Lock()
			return errors.New("vault 数据解析失败")
		}
	}
	v.creds = creds
	return nil
}

func (v *Vault) Lock() {
	if v == nil {
		return
	}
	if v.masterKey != nil {
		for i := range v.masterKey {
			v.masterKey[i] = 0
		}
	}
	v.masterKey = nil
	v.creds = nil
}

func (v *Vault) Set(cred *Credential) error {
	if cred == nil || cred.Name == "" {
		return errors.New("凭据名称不能为空")
	}
	if err := v.ensureUnlocked(); err != nil {
		return err
	}

	if v.creds == nil {
		v.creds = map[string]*Credential{}
	}
	copyCred := *cred
	v.creds[cred.Name] = &copyCred
	return v.persist()
}

func (v *Vault) Get(name string) (*Credential, error) {
	if err := v.ensureUnlocked(); err != nil {
		return nil, err
	}
	cred, ok := v.creds[name]
	if !ok {
		return nil, errors.New("凭据不存在")
	}
	copyCred := *cred
	return &copyCred, nil
}

func (v *Vault) Delete(name string) error {
	if err := v.ensureUnlocked(); err != nil {
		return err
	}
	delete(v.creds, name)
	return v.persist()
}

func (v *Vault) ensureUnlocked() error {
	if v == nil {
		return errors.New("vault 未初始化")
	}
	if len(v.masterKey) == 0 || v.creds == nil {
		return errors.New("vault 未解锁")
	}
	return nil
}

func (v *Vault) persist() error {
	if err := v.ensureUnlocked(); err != nil {
		return err
	}
	if len(v.salt) != saltSize {
		return errors.New("vault salt 无效")
	}

	dir := filepath.Dir(v.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	plain, err := json.Marshal(v.creds)
	if err != nil {
		return err
	}

	cipherBlob, err := encrypt(v.masterKey, plain)
	if err != nil {
		return err
	}

	payload := append(append([]byte{}, v.salt...), cipherBlob...)
	return os.WriteFile(v.path, payload, 0o600)
}

func deriveKey(masterPassword string, salt []byte) []byte {
	return pbkdf2.Key([]byte(masterPassword), salt, pbkdf2Iter, keySize, sha256.New)
}

func encrypt(key []byte, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	return append(nonce, ciphertext...), nil
}

func decrypt(key []byte, cipherBlob []byte) ([]byte, error) {
	if len(cipherBlob) < nonceSize {
		return nil, errors.New("cipher text too short")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := cipherBlob[:nonceSize]
	ciphertext := cipherBlob[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
