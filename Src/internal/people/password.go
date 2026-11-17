package people

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	// ProdCost 是生产环境的 bcrypt 代价. 需要在 J1800 上保持单次大约 250 毫秒以内.
	ProdCost = 10
	// MinCost 只给测试用.
	MinCost      = bcrypt.MinCost
	minPassword  = 8
	maxPassword  = 72
	passwordSize = 12
)

var (
	ErrPassword = errors.New("密码长度必须在 8 到 72 字节之间")
	hashSlots   = make(chan struct{}, 2)
	alphabet    = []byte("ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789")
)

// ValidatePassword 检查长度. bcrypt 超过 72 字节会截断或拒绝.
func ValidatePassword(password string) error {
	n := len(password)
	if n < minPassword || n > maxPassword {
		return ErrPassword
	}
	return nil
}

// HashPassword 计算哈希. 同时最多两个人在算.
func HashPassword(ctx context.Context, password string, cost int) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	if err := takeSlot(ctx); err != nil {
		return "", err
	}
	defer releaseSlot()
	sum, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", fmt.Errorf("计算密码哈希: %w", err)
	}
	return string(sum), nil
}

// CheckPassword 校验密码. 失败时返回 false, 不区分原因.
func CheckPassword(ctx context.Context, hash, password string) (bool, error) {
	if len(password) > maxPassword {
		return false, nil
	}
	if err := takeSlot(ctx); err != nil {
		return false, err
	}
	defer releaseSlot()
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// InitialPassword 生成 8 位初始密码, 同时含大写, 小写和数字.
func InitialPassword() (string, error) {
	upper := []byte("ABCDEFGHJKLMNPQRSTUVWXYZ")
	lower := []byte("abcdefghijkmnopqrstuvwxyz")
	digit := []byte("23456789")
	all := append(append(append([]byte{}, upper...), lower...), digit...)
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	out := []byte{
		upper[int(raw[0])%len(upper)],
		lower[int(raw[1])%len(lower)],
		digit[int(raw[2])%len(digit)],
	}
	for i := 3; i < 8; i++ {
		out = append(out, all[int(raw[i])%len(all)])
	}
	var mix [8]byte
	if _, err := rand.Read(mix[:]); err != nil {
		return "", err
	}
	for i := len(out) - 1; i > 0; i-- {
		j := int(mix[i]) % (i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}

// RandomPassword 生成一次性初始密码.
func RandomPassword() (string, error) {
	var raw [passwordSize]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	out := make([]byte, passwordSize)
	for i, b := range raw {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}

func takeSlot(ctx context.Context) error {
	select {
	case hashSlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseSlot() {
	<-hashSlots
}
