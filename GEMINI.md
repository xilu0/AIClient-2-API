# AIClient-2-API Context

## Project Overview

**AIClient-2-API** is a powerful Node.js-based API proxy service designed to unify various client-only large model APIs (like Gemini CLI, Antigravity, Qwen Code, Kiro) into a standard OpenAI-compatible interface. This allows tools like Cherry-Studio, NextChat, and Cline to access advanced models that might otherwise be restricted or difficult to integrate.

## Key Features

*   **Unified Interface:** OpenAI-compatible API for multiple providers.
*   **Protocol Conversion:** Intelligent conversion between OpenAI, Claude, and Gemini protocols.
*   **Web UI:** Built-in management console for configuration and monitoring (default port 3000).
*   **Provider Management:** Support for multiple accounts, polling, failover, and health checks.
*   **Plugin System:** Modular architecture for extending functionality.
*   **Containerization:** Full Docker support for easy deployment.

## Architecture & Tech Stack

*   **Runtime:** Node.js (v20+).
*   **Server Framework:** Native Node.js `http` module (no Express).
*   **Process Model:** **Master-Worker** architecture.
    *   **Master Process (`src/core/master.js`):** Manages the worker process, handles restarts, and provides a management API (default port 3100).
    *   **Worker Process (`src/services/api-server.js`):** The actual API server handling requests (default port 3000).
*   **Testing:** Jest framework (`tests/`).
*   **Configuration:** JSON-based configuration in `configs/`, managed by `src/core/config-manager.js`.

## Directory Structure

*   `src/`
    *   `core/`: Core logic including `master.js` (entry point), `config-manager.js`, and `plugin-manager.js`.
    *   `services/`: Service layer including `api-server.js` (worker entry), `api-manager.js`, and `ui-manager.js`.
    *   `handlers/`: Request handling logic (`request-handler.js`).
    *   `providers/`: Adapter implementations for different AI providers (Gemini, Claude, OpenAI, etc.).
    *   `converters/`: Logic for converting between different API protocols.
    *   `plugins/`: Plugin implementations.
    *   `ui-modules/`: Backend logic for the Web UI.
    *   `utils/`: Utility functions.
    *   `auth/`: OAuth handling.
*   `configs/`: Configuration files (e.g., `config.json`, `provider_pools.json`).
*   `static/`: Frontend assets for the Web UI.
*   `docker/`: Docker related files and documentation.
*   `tests/`: Unit and integration tests.

## Key Commands

### Development & Running
*   **Start (Master + Worker):** `npm start` (Runs `node src/core/master.js`)
*   **Start Standalone (Worker only):** `npm run start:standalone` (Runs `node src/services/api-server.js`)
*   **Development Mode:** `npm run start:dev` (Runs master with `--dev` flag)

### Testing
*   **Run All Tests:** `npm test` (Runs Jest)
*   **Watch Mode:** `npm run test:watch`
*   **Unit Tests:** `npm run test:unit`
*   **Integration Tests:** `npm run test:integration`

### Docker
*   **Build & Run:** See `docker/docker-compose.yml` or `install-and-run.sh`.

## Conventions

*   **Code Style:** Standard Node.js ES modules (`import`/`export`).
*   **Configuration:** Use `configs/config.json` for persistent settings. Environment variables are also supported via `dotenv`.
*   **Logging:** Configurable logging (console/file) for requests and debugging.
