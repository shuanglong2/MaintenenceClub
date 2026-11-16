package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/config"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/db"
)

func cmdAdmin(args []string) error {
	if len(args) < 2 {
		usage()
		return errors.New("参数不足")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	svc, err := people.New(pool, people.ProdCost)
	if err != nil {
		return err
	}
	switch args[0] {
	case "create":
		password, err := people.PromptPassword("密码: ")
		if err != nil {
			return err
		}
		again, err := people.PromptPassword("再输入一次: ")
		if err != nil {
			return err
		}
		if password != again {
			return errors.New("两次密码不一致")
		}
		if err := svc.CreateFirst(ctx, args[1], password); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "已创建组长 %s\n", args[1])
		return nil
	case "reset-password":
		password, err := svc.AdminReset(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Println(password)
		return nil
	default:
		usage()
		return errors.New("未知的管理命令")
	}
}
