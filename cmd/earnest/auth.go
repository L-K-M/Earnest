package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"golang.org/x/term"
)

func readPassword() (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(b), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func authCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("identity command required")
	}
	ctx := context.Background()
	dsn := os.Getenv("EARNEST_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("EARNEST_DATABASE_URL is required")
	}
	db, err := openDatabase(ctx, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	a := service.NewAuth(db, time.Now, []byte(os.Getenv("EARNEST_SECRET_KEY")))
	switch args[0] {
	case "user":
		if len(args) != 3 {
			return fmt.Errorf("usage: earnest user create|passwd <name>")
		}
		pass, err := readPassword()
		if err != nil {
			return err
		}
		switch args[1] {
		case "create":
			_, err = a.CreateUser(ctx, args[2], pass)
		case "passwd":
			err = a.SetPassword(ctx, args[2], pass)
		default:
			return fmt.Errorf("unknown user command")
		}
		return err
	case "compat-password":
		fs := flag.NewFlagSet("compat-password", flag.ContinueOnError)
		user := fs.String("user", "", "username")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *user == "" || fs.NArg() != 0 {
			return fmt.Errorf("usage: earnest compat-password --user <name>")
		}
		actor, err := a.UserActor(ctx, *user)
		if err != nil {
			return err
		}
		secret, err := a.SetCompatPassword(ctx, actor)
		if err != nil {
			return err
		}
		fmt.Println(secret)
		return nil
	case "token":
		return tokenCommand(ctx, a, args[1:])
	default:
		return fmt.Errorf("unknown identity command")
	}
}

func tokenCommand(ctx context.Context, a *service.Auth, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("token command required")
	}
	if args[0] == "revoke" {
		if len(args) != 2 {
			return fmt.Errorf("usage: earnest token revoke <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("invalid token id")
		}
		return a.RevokeTokenByID(ctx, id)
	}
	fs := flag.NewFlagSet("token "+args[0], flag.ContinueOnError)
	user := fs.String("user", "", "username")
	kind := fs.String("kind", "agent", "token kind")
	name := fs.String("name", "", "token name")
	scopes := fs.String("scope", "", "comma-separated scopes")
	audience := fs.String("audience", "api", "audience")
	budget := fs.Int("budget", 3, "daily notification budget")
	expires := fs.String("expires", "", "expiry duration")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *user == "" || fs.NArg() != 0 {
		return fmt.Errorf("username required")
	}
	actor, err := a.UserActor(ctx, *user)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		tokens, err := a.ListTokens(ctx, actor)
		if err != nil {
			return err
		}
		for _, t := range tokens {
			fmt.Printf("%d\t%s\t%s\t%s\n", t.ID, t.Kind, t.Audience, t.Name)
		}
		return nil
	case "create":
		opts := service.TokenOptions{Kind: service.TokenKind(*kind), Name: *name, Audience: service.Audience(*audience), DailyBudget: *budget}
		if *scopes != "" {
			for _, scope := range strings.Split(*scopes, ",") {
				opts.Scopes = append(opts.Scopes, service.Scope(strings.TrimSpace(scope)))
			}
		}
		if *expires != "" {
			value := *expires
			if strings.HasSuffix(value, "d") {
				days, err := strconv.ParseInt(strings.TrimSuffix(value, "d"), 10, 32)
				if err != nil || days <= 0 || days > 365000 {
					return fmt.Errorf("invalid expiry")
				}
				value = fmt.Sprintf("%dh", days*24)
			}
			duration, err := time.ParseDuration(value)
			if err != nil || duration <= 0 {
				return fmt.Errorf("invalid expiry")
			}
			when := time.Now().Add(duration)
			opts.ExpiresAt = &when
		}
		token, err := a.CreateToken(ctx, actor, opts)
		if err != nil {
			return err
		}
		fmt.Println(token.Secret)
		return nil
	default:
		return fmt.Errorf("unknown token command")
	}
}
