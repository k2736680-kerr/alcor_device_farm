package consoleauth

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"golang.org/x/crypto/argon2"
	"gopkg.in/yaml.v3"
)

type User struct {
	ID           string           `yaml:"id"`
	DisplayName  string           `yaml:"display_name"`
	Role         auth.ConsoleRole `yaml:"role"`
	PasswordHash string           `yaml:"password_hash"`
}

type userFile struct {
	Users []User `yaml:"users"`
}

func LoadUsers(path string) (map[string]User, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat console users file: %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("console users file must not be accessible by group or other users")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read console users file: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var document userFile
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode console users file: %w", err)
	}
	if len(document.Users) == 0 {
		return nil, errors.New("console users file must contain at least one user")
	}
	users := make(map[string]User, len(document.Users))
	for _, user := range document.Users {
		user.ID = strings.TrimSpace(user.ID)
		user.DisplayName = strings.TrimSpace(user.DisplayName)
		if user.ID == "" || len(user.ID) > 64 || user.DisplayName == "" || len(user.DisplayName) > 128 {
			return nil, errors.New("console user id and display_name are required and must fit configured limits")
		}
		switch user.Role {
		case auth.ConsoleViewer, auth.ConsoleOperator, auth.ConsoleAdmin:
		default:
			return nil, fmt.Errorf("console user %q has invalid role", user.ID)
		}
		if _, _, _, err := parseArgon2id(user.PasswordHash); err != nil {
			return nil, fmt.Errorf("console user %q password hash: %w", user.ID, err)
		}
		if _, exists := users[user.ID]; exists {
			return nil, fmt.Errorf("duplicate console user id %q", user.ID)
		}
		users[user.ID] = user
	}
	return users, nil
}

func VerifyPassword(encoded, password string) bool {
	parameters, salt, expected, err := parseArgon2id(encoded)
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, parameters.time, parameters.memory, parameters.threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

type argon2Parameters struct {
	memory  uint32
	time    uint32
	threads uint8
}

func parseArgon2id(encoded string) (argon2Parameters, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return argon2Parameters{}, nil, nil, errors.New("must use Argon2id PHC format version 19")
	}
	var parameters argon2Parameters
	values := strings.Split(parts[3], ",")
	if len(values) != 3 {
		return argon2Parameters{}, nil, nil, errors.New("invalid Argon2id parameters")
	}
	for _, value := range values {
		pair := strings.SplitN(value, "=", 2)
		if len(pair) != 2 {
			return argon2Parameters{}, nil, nil, errors.New("invalid Argon2id parameter")
		}
		number, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return argon2Parameters{}, nil, nil, errors.New("invalid Argon2id parameter value")
		}
		switch pair[0] {
		case "m":
			parameters.memory = uint32(number)
		case "t":
			parameters.time = uint32(number)
		case "p":
			if number > 255 {
				return argon2Parameters{}, nil, nil, errors.New("invalid Argon2id parallelism")
			}
			parameters.threads = uint8(number)
		default:
			return argon2Parameters{}, nil, nil, errors.New("unknown Argon2id parameter")
		}
	}
	if parameters.memory < 19*1024 || parameters.memory > 1024*1024 || parameters.time < 2 || parameters.time > 10 || parameters.threads < 1 || parameters.threads > 16 {
		return argon2Parameters{}, nil, nil, errors.New("Argon2id parameters are outside accepted limits")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 {
		return argon2Parameters{}, nil, nil, errors.New("invalid Argon2id salt")
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) < 32 {
		return argon2Parameters{}, nil, nil, errors.New("invalid Argon2id hash")
	}
	return parameters, salt, hash, nil
}
