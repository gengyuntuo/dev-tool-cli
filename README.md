# dev-tool-cli

A Go TUI application scaffold using Bubble Tea with SSH tunnel support.

## Dependencies

- `github.com/charmbracelet/bubbletea`
- `golang.org/x/crypto/ssh`

## Structure

- `main.go` keeps only the application entry point.
- `internal/app` contains TUI model, state handling, and render logic.
- `internal/emr` encapsulates EMR API calls.
- `internal/tunnel` encapsulates SSH remote-forward logic.

## Run

```bash
go run .
```
