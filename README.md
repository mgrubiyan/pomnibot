# Pomnibot

Flashcard self-testing service and bot for the MAX messenger, generating strictly factual question cards from student notes with source citations.

## Task Runner Setup

The project uses [Task](https://taskfile.dev) for task automation. Install it via Go:

```bash
go install github.com/go-task/task/v3/cmd/task@latest
```

Ensure the Go binary directory is added to your `PATH`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Usage

List all available tasks:

```bash
task --list
```

### Development & Build

- `task dev` — run frontend dev server
- `task build` — build frontend
- `task lint` — run linters across frontend and backend
- `task test` — run backend unit tests
- `task up` — run services in Docker Compose
- `task down` — stop Docker Compose
