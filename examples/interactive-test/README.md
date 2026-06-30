# interactive-test

Manual smoke test for the [interactive message helpers](../../docs/interactive_messages.md).
It logs in via QR code (printed as ASCII in the terminal) and sends a batch of
button messages to a recipient.

> ⚠️ **EXPERIMENTAL — account-ban risk.** Sending interactive messages from
> non-official libraries is experimental and WhatsApp may **ban** the sending
> account. Scan the QR code **only with a disposable test account**. **Never use
> the production (atendzappy) account.**

## Run

```sh
# recipient phone number, digits only, no "+" (example value below)
export TEST_RECIPIENT=5577988272902
go run ./examples/interactive-test
```

On Windows PowerShell:

```powershell
$env:TEST_RECIPIENT = "5577988272902"
go run ./examples/interactive-test
```

1. Scan the **ASCII QR** shown in the terminal with your **test** WhatsApp account
   (this is the *sending* account). If your terminal can't render the blocks, set
   `QR_PNG=1` to also write a scannable `qr.png` (gitignored) you can open in an
   image viewer.
2. The program resolves the recipient's canonical JID (`IsOnWhatsApp`) — this
   handles the Brazilian 9th-digit case — then sends the test messages and logs
   each result.
3. Press `Ctrl+C` to disconnect and exit.

The session is cached in `session.db` (gitignored), so the QR is only needed on
the first run. Delete that file to force a fresh login.

## Environment variables

| Var | Effect |
| --- | --- |
| `TEST_RECIPIENT` | Recipient phone number, digits only, no `+`. Comma-separate several (useful with `CHECK_ONLY`). The first one is the send target. |
| `QR_PNG=1` | Also write `qr.png` in addition to the ASCII QR. |
| `CHECK_ONLY=1` | Only run the `IsOnWhatsApp` lookup for each number and exit — sends nothing. |
| `LISTEN_ONLY=1` | Connect and only listen for/log incoming button responses (no sending). Tap a rendered button on the recipient to see its payload here. |

## What renders

This fork patches the send core (`send_interactive_patch.go`) so all five types
get a `<biz>` routing node. **However, field testing showed that a non-Business
sender's buttons still do not render as clickable** on a normal recipient — they
arrive as "waiting for this message" / "your WhatsApp version is not compatible",
and lists degrade to plain text. WhatsApp gates interactive-button rendering to
official Business API senders. See
[docs/interactive_messages_test_report.md](../../docs/interactive_messages_test_report.md)
for the full before/after report.

## Notes

- Uses the pure-Go `modernc.org/sqlite` driver, so no C compiler is needed.
- The recipient number is read from `TEST_RECIPIENT` and is **not** hardcoded.
- This example is for manual runs only; it is not part of CI.
