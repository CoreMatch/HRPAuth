# HRPAuth

HRPAuth is designed to provide high-performance and high-security authentication and skin management for Minecraft and its related ecosystem.

## Core Features

- **Yggdrasil API Support**
    - Fully implements the official Minecraft authentication protocol (Authenticate, Refresh, Validate, Invalidate).
    - Supports binding local accounts with official Mojang UUIDs and handling name conflicts.
- **Skin and Texture Management**
    - Supports uploading, deleting, and retrieving skins and capes.
    - Strict texture validation logic with RSA digital signatures to ensure the legitimacy of texture data.


## Tech Stack

- **Language**: Go (Golang) 1.26+
- **Framework**: [Gin](https://github.com/gin-gonic/gin) (Web Framework)
- **Database**: MySQL (Main Storage), Redis (Caching & Rate Limiting)
- **ORM**: [GORM](https://gorm.io/)
- **Migrations**: [golang-migrate](https://github.com/golang-migrate/migrate)
- **Encryption**: bcrypt (Password Hashing), RSA (Digital Signatures)

## Quick Start

### 1. Prerequisites
- Go 1.26 or higher
- MySQL 5.7+
- Redis 6.0+

### 2. Clone and Run
```bash
# Clone the repository
git clone https://github.com/CoreMatch/HRPAuth.git`
cd HRPAuth

# First attempt to run (automatically generates configuration and RSA keys)
go run main.go
```  
Build and run the application:

```bash
go build -o HRPAuth
./HRPAuth
```

### 3. Configuration
After the first run, the following files will be generated in the project root:
- `config.yaml`: Main configuration file.
- `public_key.pem`: RSA public key (used for Yggdrasil signatures).
- `private_key.pem`: RSA private key.

Please modify the `database`, `redis`, and `smtp` settings in `config.yaml` according to your environment, then restart the application.

## Project Structure

```text
├── config/             # Configuration loading and migration logic
├── controllers/        # API controllers (handling HTTP requests)
├── database/           # Database initialization and SQL migration files
├── models/             # GORM data model definitions
├── redis/              # Redis client initialization
├── services/           # Core business logic (Auth, Email, Texture processing)
├── utils/              # Common utility functions
├── main.go             # Application entry point
└── go.mod              # Dependency management
```

## Documentation

All API documentation and development specifications are centralized in the HA-Contract root directory:
- [API Standards & Specifications](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/README.md)
- [OpenAPI Definition](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/openapi/hrpauth-business.yaml)
- [Error Codes](https://github.com/CoreMatch/HA-Contract/blob/main/docs/api/error-codes.md)

## Related Projects
- [HRPAuth-WebUI](https://github.com/CoreMatch/HRPAuth-WebUI): The official webUI repository for HRPAuth.
- [WinnerProxy](https://github.com/CoreMatch/WinnerProxy): It provides a proxy service for HRPAuth, supporting both HRPAuth and Mojang official authentication protocols for users to join the server.
- [HASkinLib](https://github.com/CoreMatch/HASkinLib): Skin and texture sharing library for HRPAuth.
- [HASkinProxy](https://github.com/CoreMatch/HASkinProxy): Make HRPAuth skin and texture API endpoint available for CustomSkinLoader.
- [BS2HA](https://github.com/CoreMatch/BS2HA): A tool to convert Blessing-Skin database to HRPAuth database.
- [HRPAuth-Wiki](https://github.com/CoreMatch/HRPAuth-Wiki): The official wiki repository for HRPAuth.
- [HA-Contract](https://github.com/CoreMatch/HA-Contract): The official contract repository for HRPAuth.

## License

This project is licensed under the [MIT License](LICENSE).
