package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"music-player/server/internal/config"
	"music-player/server/internal/store"
)

func runAccounts(args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("用法: music-player account add|reset|disable|list --data-dir <数据目录> [--id <用户标识>] [--name <显示名>] [--account-stdin]")
	}
	action := args[0]
	if action != "add" && action != "reset" && action != "disable" && action != "list" {
		return errors.New("未知账号管理命令")
	}
	flags := flag.NewFlagSet("account "+action, flag.ContinueOnError)
	flags.SetOutput(output)
	id := flags.String("id", "", "稳定用户标识；现有音乐库使用 default-user")
	name := flags.String("name", "", "显示名，不作为登录凭据")
	fromStdin := flags.Bool("account-stdin", false, "从标准输入读取指定账号，默认生成随机账号")
	common := map[string]*string{}
	for _, key := range []string{"config", "data-dir", "database"} {
		common[key] = flags.String(key, "", "与服务端相同的配置选项")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("不接受额外位置参数")
	}
	if action != "list" && strings.TrimSpace(*id) == "" {
		return errors.New("必须通过 --id 指定用户标识；原音乐库使用 default-user")
	}
	var configArgs []string
	flags.Visit(func(f *flag.Flag) {
		if v, ok := common[f.Name]; ok {
			configArgs = append(configArgs, "--"+f.Name, *v)
		}
	})
	cfg, err := config.Load(configArgs)
	if err != nil {
		return err
	}
	var account string
	if action == "add" || action == "reset" {
		if *fromStdin {
			data, err := io.ReadAll(io.LimitReader(input, 258))
			if err != nil {
				return err
			}
			account = strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
			if len(account) > 256 || strings.TrimSpace(account) == "" {
				return errors.New("账号不能为空且最多 256 字节")
			}
		} else {
			account, err = store.RandomCredential()
			if err != nil {
				return err
			}
		}
	}
	if err = os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0700); err != nil {
		return err
	}
	ctx := context.Background()
	s, err := store.Open(ctx, cfg.DatabasePath, store.WithDataRoot(cfg.DataDir))
	if err != nil {
		return err
	}
	defer s.Close()
	switch action {
	case "list":
		items, err := s.Accounts(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(items)
	case "disable":
		if err = s.DisableAccount(ctx, *id); err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "账号已停用，已有会话已撤销，歌曲和设置保留。")
		return err
	default:
		if err = s.SetAccount(ctx, *id, *name, account, action == "add"); err != nil {
			return err
		}
		if *fromStdin {
			_, err = fmt.Fprintln(output, "账号已设置，已有会话已撤销。")
		} else {
			_, err = fmt.Fprintf(output, "用户: %s\n登录账号（仅显示这一次）: %s\n", *id, account)
		}
		return err
	}
}
