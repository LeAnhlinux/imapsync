# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

imapsync is a single-file Perl tool (~22,000 lines in `./imapsync`) for syncing, copying, and migrating email between two IMAP servers. Version 2.229.

## Common Commands

```bash
# Check Perl prerequisites (missing modules)
make testp

# Run built-in unit tests (embedded in imapsync itself)
./imapsync --tests

# Run built-in debug tests
./imapsync --testsdebug

# Run live tests against remote test servers
./imapsync --testslive

# Run the full shell test suite (requires local IMAP servers with test accounts)
sh tests.sh

# Run a specific test from the test suite
sh tests.sh <test_name>    # e.g., sh tests.sh ll_ssl_justconnect

# Quick local test
make test_quick

# Run tests (output suppressed, creates .tests_passed on success)
make test

# Verbose tests
make testv

# Install (as root, installs to /usr/bin by default)
make install

# Generate docs (README from pod, man page, HTML tutorials)
make doc

# Run perlcritic
make crit

# Syntax check
perl -c imapsync
```

## Architecture

- **`imapsync`** - The entire application is a single Perl script. Structure follows this order: pod documentation, use pragmas, main program, global variables initialization, `get_options()`, default values, folder loop, subroutines, `sub usage`.
- **`tests.sh`** - Shell-based integration test suite. Tests require running IMAP servers with specific test accounts. Each test is a shell function; `run_tests()` executes them and tracks pass/fail. Uses `HOST1`/`HOST2` env vars (default: localhost).
- **`oauth2/`** - OAuth2 helper scripts for Gmail authentication (`generate_gmail_token`, `oauth2_imap_from_token`).
- **`examples/`** - Example sync scripts for various platforms (Unix, macOS, Windows), including parallel sync and archive patterns.
- **`INSTALL.d/`** - Platform-specific install docs, Dockerfile, and `prerequisites_imapsync` script.
- **`webserver/`** - CGI-based web UI for running imapsync online.

## Key Perl Dependencies

Mail::IMAPClient (>= 3.30), IO::Socket::SSL, IO::Socket::INET6, Encode::IMAPUTF7, File::Copy::Recursive, IO::Tee, Compress::Zlib, Authen::NTLM, Data::Uniqid, Unicode::String.

## Testing Notes

- The `--tests` flag runs imapsync's internal unit tests (no external server needed).
- The `--testslive` flag runs live tests against `test1.lamiral.info` / `test2.lamiral.info`.
- `tests.sh` tests are environment-specific and depend on local IMAP server content. The `CMD_PERL` var in tests.sh points to a bundled Mail::IMAPClient: `perl -I./W/Mail-IMAPClient-3.43/lib`.
