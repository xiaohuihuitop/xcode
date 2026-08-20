# platform-rebuild

This command is the manual cutover tool for the `my2.0` platform model.

It is read-only by default:

```text
go run ./cmd/platform-rebuild
```

After taking and verifying a database backup, apply the cutover explicitly:

```text
go run ./cmd/platform-rebuild --apply
```

The apply mode removes API-key-to-platform rows, disables existing platform
rules and platforms, and disables accounts that were attached to a platform.
It does not delete users, API keys, balances, plans, subscriptions, payment
records, or usage history. Administrators must create/enable the new platforms,
re-add accounts, and authorize API keys afterward.
