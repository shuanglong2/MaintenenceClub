package people

import (
	"context"
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("1234567"); err == nil {
		t.Fatal("短密码应拒绝")
	}
	if err := ValidatePassword(strings.Repeat("a", 73)); err == nil {
		t.Fatal("超长密码应拒绝")
	}
	if err := ValidatePassword("12345678"); err != nil {
		t.Fatal(err)
	}
}

func TestHashAndCheck(t *testing.T) {
	ctx := context.Background()
	hash, err := HashPassword(ctx, "correct-horse", MinCost)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := CheckPassword(ctx, hash, "correct-horse")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	ok, err = CheckPassword(ctx, hash, "wrong-horse")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestRandomPassword(t *testing.T) {
	password, err := RandomPassword()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePassword(password); err != nil {
		t.Fatal(err)
	}
}
