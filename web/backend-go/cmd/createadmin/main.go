// Command createadmin creates, or resets the password of, an admin user.
//
// Self-registration is disabled in the API, so this is the only way to get the
// first account into a fresh install.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/imapsync/web-backend/internal/config"
	"github.com/imapsync/web-backend/internal/security"
	"github.com/imapsync/web-backend/internal/store"
)

func main() {
	username := flag.String("username", "", "admin username")
	email := flag.String("email", "", "admin email")
	password := flag.String("password", "", "admin password (prompted if omitted; shows up in shell history)")
	flag.Parse()

	if err := run(*username, *email, *password); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(username, email, password string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	in := bufio.NewReader(os.Stdin)
	if username == "" {
		username = prompt(in, "Username: ")
	}
	if email == "" {
		email = prompt(in, "Email: ")
	}
	if username == "" || email == "" {
		return errors.New("username and email are required")
	}

	if password == "" {
		fmt.Print("Password: ")
		first, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return err
		}
		fmt.Print("Confirm password: ")
		second, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return err
		}
		if string(first) != string(second) {
			return errors.New("passwords do not match")
		}
		password = string(first)
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}

	if _, err := security.New(cfg.SecretKey, cfg.FernetKey, cfg.AccessTokenTTL); err != nil {
		return err
	}
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer db.Close()

	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}

	if existing, err := db.UserByUsername(username); err == nil {
		if err := db.SetUserPassword(existing.ID, hash, true); err != nil {
			return err
		}
		fmt.Printf("Updated password for existing user %q.\n", username)
		return nil
	}

	if err := db.CreateUser(&store.User{
		Username: username, Email: email, HashedPassword: hash, IsAdmin: true,
	}); err != nil {
		return err
	}
	fmt.Printf("Admin user %q created.\n", username)
	return nil
}

func prompt(in *bufio.Reader, label string) string {
	fmt.Print(label)
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}
