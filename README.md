# Automation Plugin for Slidebolt

The Automation Plugin provides logic-heavy automation capabilities for the Slidebolt ecosystem. It handles complex device interactions and user-defined rules.

## Features

- **Logic Engine**: Implements complex automation rules and device-to-device interactions.
- **Isolated Service**: Runs as a standalone sidecar service communicating via NATS.

## Architecture

This plugin follows the Slidebolt "Isolated Service" pattern:
- **`pkg/bundle`**: Implementation of the `sdk.Plugin` interface.
- **`pkg/logic`**: Core automation and rule processing logic.
- **`cmd/main.go`**: Service entry point.

## Development

### Prerequisites
- Go (v1.25.6+)
- Slidebolt `plugin-sdk` and `plugin-framework` repos sitting as siblings.

### Local Build
Initialize the Go workspace to link sibling dependencies:
```bash
go work init . ../plugin-sdk ../plugin-framework
go build -o bin/plugin-automation ./cmd/main.go
```

### Testing
```bash
go test ./...
```

## Docker Deployment

### Build the Image
To build with local sibling modules:
```bash
make docker-build-local
```

To build from remote GitHub repositories:
```bash
make docker-build-prod
```

### Run via Docker Compose
Add the following to your `docker-compose.yml`:
```yaml
services:
  automation:
    image: slidebolt-plugin-automation:latest
    environment:
      - NATS_URL=nats://core:4222
    restart: always
```

## License
Refer to the root project license.
