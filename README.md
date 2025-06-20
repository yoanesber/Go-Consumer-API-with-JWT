# Transaction Processing Service (Go + Redis + PostgreSQL + Kafka)

This service is responsible for processing **financial transactions** in a **secure** and **idempotent** manner. Each transaction request must include a valid **JWT token** and an **Idempotency-Key** to ensure safe retries and prevent duplicate processing. The system uses **Redis** for fast **idempotency checking** and **Kafka** for **asynchronous background processing** of pending transactions.

Incoming transactions are **validated, persisted, and queued** for processing. A background scheduler pulls **"processing"** transactions periodically and attempts to complete them via a third-party API.

---


## ✨ Features

This application provides a **secure**, **token-based authentication system using JWT (JSON Web Tokens)**, **fully integrated with Redis** for optimized token handling, and **PostgreSQL** for persistent storage. Below is a summary of the core features offered:

### 🔐 JWT Authentication

- Full **JWT authentication** system: 
  - This service secures its API endpoints using JWT (JSON Web Token). Every request must include a valid JWT token in the `Authorization` header (`Bearer <token>`).
  - The token is verified before any processing happens, and requests with missing or invalid tokens receive a `401 Unauthorized` response.
  - Ensure only authenticated users can interact with the transaction API.
  - Helps trace and attribute transactions to specific authenticated consumers or services.
  - `POST /auth/login` — Accepts `username` and `password`, returns:
    - `AccessToken`
    - `RefreshToken`
    - `ExpirationDate`
    - `TokenType`
  - `POST /auth/refresh-token` — Accepts valid `RefreshToken` to generate new `AccessToken`.

- **Token storage in Redis** for faster access:
  - Stored under key format: `access_token:<username>`
  - JSON structure: `{ AccessToken, RefreshToken, ExpirationDate, TokenType }`

- **RSA key pairs** are used for signing JWTs (instead of symmetric secrets)
- Keys are generated using OpenSSL:
  - `privateKey.pem`, `publicKey.pem` in `/keys`


### 🛡️ Security & Middleware

The service is designed with security and extensibility in mind, using several middlewares:

- **Authorization Middleware**:
  - Validates JWT
  - Enforces Role-Based Access Control (RBAC)

- **Security Headers Middleware**:
  - CORS
  - Secure HTTP headers (e.g., `X-Frame-Options`, `X-Content-Type-Options`, etc.)

- **Rate Limiter**:
  - Built on `golang.org/x/time/rate`
  - Rate limits based on unique key: `IP + HTTP method + route path`


### ♻️ Idempotency Enforcement  

Each transaction request must include an `Idempotency-Key` (UUID). The service ensures the same key cannot be used to create multiple logically different transactions, preventing accidental duplicates on retries.  
- How it works:
  - The raw request body is hashed (SHA-256).
  - Redis is queried for `idempotency_cache:<Idempotency-Key>`.  
  - Purpose:  
    - Prevents duplicate charges/payments on retries.  
    - Guarantees safe retries on network failure or client timeouts.  
  - All idempotency data (key, hash, and response) is saved in `PostgreSQL` and also cached in `Redis` for fast lookup. This dual approach ensures:
    - Durability (in DB)
    - Performance (in Redis)


### 📬 Kafka Integration for Async Event-Driven Processing

- Once a new transaction is stored, an event containing the transaction metadata is published to a Kafka topic based on its type:
  - `payment-event`, `withdrawal-event`, or `disbursement-event`
- Kafka consumers listen to these topics, extract the event, and begin processing it asynchronously
- Purpose:
  - Decouples immediate request handling from long-running transaction finalization.
  - Enables scalability and retry-friendly design via Kafka's durability


### ⏱ Goroutine-Based Periodic Scheduler

A built-in scheduler runs every 5 seconds using goroutines to process transactions in the `"processing"` state. Each cycle:
- Queries at most 5 processing transactions
- Sends them to external services
- Updates their status to `"completed"` or `"failed"` based on the response
- Purpose:
  - Allows distributed or delayed processing independent of user requests
  - Ensures eventual consistency and resilience if previous attempts failed


### 🗄️ Logging

- Uses `github.com/sirupsen/logrus` for structured logging
- Integrates with `gopkg.in/natefinch/lumberjack.v2` for automatic log rotation based on size and age
- Logs are separated by level: **info**, **request**, **warn**, **error**, **fatal**, and **panic**


---

## 🧭 Business Process Flow

The following diagram illustrates the end-to-end flow of how a new transaction request is handled by the system, from initial client submission to background processing and external integration. It highlights key components such as authentication, idempotency validation, asynchronous messaging with Kafka, and scheduled processing of pending transactions.

```pgsql
┌──────────────────────────────────────────────┐
│            [1] Client Sends Request          │
│----------------------------------------------│
│ - POST /transactions                         │
│ - Headers:                                   │
│   - Authorization: Bearer <JWT>              │
│   - Idempotency-Key: <UUID>                  │
│ - Body: { type, amount, consumerId }         │
└──────────────────────────────────────────────┘
              │
              ▼
┌──────────────────────────────────────────────┐
│  [2] Middleware: Validate JWT & Idempotency  │
│----------------------------------------------│
│ - Check JWT validity → if invalid → 401      │
│ - Check Idempotency-Key format → if invalid →│
│   400                                        │
└──────────────────────────────────────────────┘
              │
              ▼
┌──────────────────────────────────────────────┐
│   [3] Redis Check: Idempotency-Key Exists?   │
│----------------------------------------------│
│ - Yes → Compare hash                         │
│   - Same → Return cached response            │
│   - Diff → Return 409 Conflict               │
│ - No  → Continue to processing               │
└──────────────────────────────────────────────┘
              │
              ▼
┌──────────────────────────────────────────────┐
│           [4] Context Injection              │
│----------------------------------------------│
│ - Inject Idempotency-Key and hashed body     │
│   into context for downstream use            │
└──────────────────────────────────────────────┘
              │
              ▼
┌──────────────────────────────────────────────┐
│     [5] Service Layer: Business Validation   │
│----------------------------------------------│
│ - Check consumerId exists → if not → 404     │
│ - Check consumer is active → if not → 400    │
└──────────────────────────────────────────────┘
              │
              ▼
┌──────────────────────────────────────────────┐
│   [6] Save Transaction & Idempotency Record  │
│----------------------------------------------│
│ - Insert into transactions (status = pending)│
│ - Insert into idempotency_cache (key, hash,  │
│   response)                                  │
│ - Save response to Redis                     │
└──────────────────────────────────────────────┘
              │
              ▼
┌──────────────────────────────────────────────┐
│      [7] Kafka: Publish Event                │
│----------------------------------------------│
│ - topic = payment-event / withdrawal-event / │
│   disbursement-event                         │
│ - Payload: transactionId, key, status, type  │
└──────────────────────────────────────────────┘
              │
              ▼
┌──────────────────────────────────────────────┐
│      [8] Kafka Consumer Handles Event        │
│----------------------------------------------│
│ - Listens on specific topic                  │
│ - Parse event & call handler                 │
│ - Handler sets status = "processing"         │
└──────────────────────────────────────────────┘
              │
              ▼
┌──────────────────────────────────────────────┐
│  [9] Scheduler (every 5s): Poll Transactions │
│----------------------------------------------│
│ - Query: transactions where status=processing│
│ - For each (limit 5):                        │
│   - Call external API                        │
│   - On success → status = completed          │
│   - On fail    → status = failed             │
└──────────────────────────────────────────────┘

```
---


## 🤖 Tech Stack

This project leverages a modern and robust set of technologies to ensure performance, security, and maintainability. Below is an overview of the core tools and libraries used in the development:

| **Component**             | **Description**                                                                             |
|---------------------------|---------------------------------------------------------------------------------------------|
| **Language**              | Go (Golang), a statically typed, compiled language known for concurrency and efficiency     |
| **Web Framework**         | Gin, a fast and minimalist HTTP web framework for Go                                        |
| **ORM**                   | GORM, an ORM library for Go supporting SQL and migrations                                   |
| **Database**              | PostgreSQL, a powerful open-source relational database system                               |
| **Cache/Session Store**   | Redis, used for caching, fast idempotency key lookup, and storing temporary session/state   |
| **JWT Signing**           | RSA asymmetric key pairs generated via OpenSSL, used to securely sign and verify JWT tokens |
| **Logging**               | Logrus for structured logging, combined with Lumberjack for log rotation                    |
| **Validation**            | `go-playground/validator.v9` for input validation and data integrity enforcement            |
| **Scheduler**             | Custom scheduler using time.Ticker + goroutines to poll pending transactions periodically   |
| **Message Broker**        | Kafka, used for publishing and consuming transaction events asynchronously                  |
| **Rate Limiting**         | `golang.org/x/time/rate` — token-bucket rate limiter to control API usage frequency         |

---

## 🧱 Architecture Overview

This project follows a **modular** and **maintainable** architecture inspired by **Clean Architecture** principles. Each domain feature (e.g., **entity**, **handler**, **repository**, **service**) is organized into self-contained modules with clear separation of concerns.

```bash
📁 go-idempotency-demo/
├── 📂cert/                                 # Stores self-signed TLS certificates used for local development (e.g., for HTTPS or JWT signing verification)
├── 📂cmd/                                  # Contains the application's entry point.
├── 📂config/
│   ├── 📂async/                            # Config for async-related components, like Kafka producer/consumer settings
│   ├── 📂cache/                            # Config for Redis (host, port, TTL, etc.)
│   └── 📂database/                         # Config for PostgreSQL (DSN, pool settings, migration, etc.)
├── 📂docker/                               # Docker-related configuration for building and running services
│   ├── 📂app/                              # Contains Dockerfile to build the main Go application image
│   ├── 📂postgres/                         # Contains PostgreSQL container configuration
│   └── 📂redis/                            # Contains Redis container configuration
├── 📂internal/                             # Core domain logic and business use cases, organized by module
│   ├── 📂entity/                           # Data models/entities representing business concepts like Transaction, Consumer
│   ├── 📂handler/                          # HTTP handlers (controllers) that parse requests and return responses
│   ├── 📂repository/                       # Data access layer, communicating with DB or cache
│   └── 📂service/                          # Business logic layer orchestrating operations between handlers and repositories
├── 📂keys/                                 # Contains RSA public/private keys used for signing and verifying JWT tokens
├── 📂logs/                                 # Application log files (error, request, info) written and rotated using Logrus + Lumberjack
├── 📂pkg/                                  # Reusable utility and middleware packages shared across modules
│   ├── 📂contextdata/                      # Stores and retrieves contextual data like Idempotency-Key, UserID, RequestID
│   ├── 📂customtype/                       # Defines custom types, enums, constants used throughout the application
│   ├── 📂diagnostics/                      # Health check endpoints, metrics, and diagnostics handlers for monitoring
│   ├── 📂kafka/
│   │   ├── 📂consumer/                     # Handles message consumption and dispatch
│   │   ├── 📂mapping/                      # Maps events between internal and Kafka schemas
│   │   ├── 📂publisher/                    # Sends messages to Kafka topics
│   │   ├── 📂schema/                       # Defines event schemas used in Kafka messaging
│   │   └── 📂validator/                    # Validates Kafka messages against schema
│   ├── 📂logger/                           # Centralized log initialization and configuration
│   ├── 📂middleware/                       # Request processing middleware
│   │   ├── 📂authorization/                # JWT validation and Role-Based Access Control (RBAC)
│   │   ├── 📂headers/                      # Manages request headers like CORS, security, request ID
│   │   ├── 📂idempotency/                  # Extracts, validates, and processes Idempotency-Key
│   │   ├── 📂logging/                      # Logs incoming requests
│   │   └── 📂ratelimiter/                  # Implements API rate limiting based on IP, path, and method
│   ├── 📂scheduler/                        # Custom background schedulers that run periodically (e.g., every 5s) to process pending transactions
│   └── 📂util/                             # General utility functions and helpers
│       ├── 📂hash-util/                    # Functions for hashing request bodies (e.g., SHA-256)
│       ├── 📂http-util/                    # Utilities for common HTTP tasks (e.g., write JSON, status helpers)
│       ├── 📂jwt-util/                     # Token generation, parsing, and validation logic
│       ├── 📂kafka-util/                   # Kafka configuration and utility helpers
│       ├── 📂redis-util/                   # Redis connection and command utilities
│       └── 📂validation-util/              # Common input validators (e.g., UUID, numeric range)
├── 📂routes/                               # Route definitions, groups APIs, and applies middleware per route scope
└── 📂tests/                                # Contains unit or integration tests for business logic
```

---

## 🛠️ Installation & Setup  

Follow the instructions below to get the project up and running in your local development environment. You may run it natively or via Docker depending on your preference.  

### ✅ Prerequisites

Make sure the following tools are installed on your system:

| **Tool**                                                      | **Description**                           |
|---------------------------------------------------------------|-------------------------------------------|
| [Go](https://go.dev/dl/)                                      | Go programming language (v1.20+)          |
| [Make](https://www.gnu.org/software/make/)                    | Build automation tool (`make`)            |
| [Redis](https://redis.io/)                                    | In-memory data store                      |
| [PostgreSQL](https://www.postgresql.org/)                     | Relational database system (v14+)         |
| [Apache Kafka](https://kafka.apache.org/)                     | Distributed event streaming platform for async processing |
| [Docker](https://www.docker.com/)                             | Containerization platform (optional)      |

### ⚙️ Configure `.env` File  

Set up your **database**, **Redis**, and **JWT configuration** in `.env` file. Create a `.env` file at the project root directory:  

```properties
# Application configuration
ENV=PRODUCTION
API_VERSION=1.0
PORT=1000
IS_SSL=TRUE
SSL_KEYS=./cert/mycert.key
SSL_CERT=./cert/mycert.cer

# Database configuration
DB_HOST=localhost
DB_PORT=5432
DB_USER=appuser
DB_PASS=app@123
DB_NAME=payment_service
DB_SCHEMA=public
DB_SSL_MODE=disable
# Options: disable, require, verify-ca, verify-full
DB_TIMEZONE=Asia/Jakarta
DB_MIGRATE=TRUE
DB_SEED=TRUE
DB_SEED_FILE=import.sql
# Set to INFO for development and staging, SILENT for production
DB_LOG=SILENT

# Redis configuration
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_USER=default
REDIS_PASS=
REDIS_DB=0
REDIS_FLUSH_DB=TRUE
# 1 hour
ACCESS_TOKEN_TTL_MINUTES=60

# Kafka configuration
KAFKA_BROKERS=localhost:9092
KAFKA_TOPICS=payment-event,withdrawal-event,disbursement-event
KAFKA_GROUP_ID=transaction-service-group
KAFKA_READ_TIMEOUT_MS=5000
KAFKA_WRITE_TIMEOUT_MS=5000
KAFKA_SSL_ENABLED=FALSE
KAFKA_SSL_CA_PATH=./cert/kafka/ca.pem
KAFKA_SSL_CERT_PATH=./cert/kafka/cert.pem
KAFKA_SSL_KEY_PATH=./cert/kafka/key.pem

# JWT configuration
JWT_SECRET=your_jwt_secret_key
# 2 days
JWT_EXPIRATION_HOUR=48
JWT_ISSUER=your_jwt_issuer
JWT_AUDIENCE=your_jwt_audience
# 30 days
JWT_REFRESH_TOKEN_EXPIRATION_HOUR=720
JWT_PRIVATE_KEY_PATH=./keys/privateKey.pem
JWT_PUBLIC_KEY_PATH=./keys/publicKey.pem
# RS256 or HS256
JWT_ALGORITHM=RS256
# Bearer or JWT
TOKEN_TYPE=Bearer

# Idempotency configuration
IDEMPOTENCY_ENABLED=TRUE
IDEMPOTENCY_KEY_HEADER=Idempotency-Key
IDEMPOTENCY_PREFIX=idempotency_cache:
IDEMPOTENCY_TTL_HOURS=24
```

- **🔐 Notes**:  
  - `IS_SSL=TRUE`: Enable this if you want your app to run over `HTTPS`. Make sure to run `generate-certificate.sh` to generate **self-signed certificates** and place them in the `./cert/` directory (e.g., `mycert.key`, `mycert.cer`).
  - `JWT_ALGORITHM=RS256`: Set this if you're using **asymmetric JWT signing**. Be sure to run `generate-jwt-key.sh` to generate **RSA key pairs** and place `privateKey.pem` and `publicKey.pem` in the `./keys/` directory.
  - Make sure your paths (`./cert/`, `./keys/`) exist and are accessible by the application during runtime.
  - `DB_TIMEZONE=Asia/Jakarta`: Adjust this value to your local timezone (e.g., `America/New_York`, etc.).
  - `DB_MIGRATE=TRUE`: Set to `TRUE` to automatically run `GORM` migrations for all entity definitions on app startup.
  - `DB_SEED=TRUE` & `DB_SEED_FILE=import.sql`: Use these settings if you want to insert predefined data into the database using the SQL file provided.
  - `DB_USER=appuser`, `DB_PASS=app@123`: It's strongly recommended to create a dedicated database user instead of using the default postgres superuser.

### 🔑 Generate RSA Key for JWT (If Using `RS256`)  

If you are using `JWT_ALGORITHM=RS256`, generate the **RSA key** pair for **JWT signing** by running this file:  
```bash
./generate-jwt-key.sh
```

- **Notes**:  
  - On **Linux/macOS**: Run the script directly
  - On **Windows**: Use **WSL** to execute the `.sh` script

This will generate:
  - `./keys/privateKey.pem`
  - `./keys/publicKey.pem`


These files will be referenced by your `.env`:
```properties
JWT_PRIVATE_KEY_PATH=./keys/privateKey.pem
JWT_PUBLIC_KEY_PATH=./keys/publicKey.pem
JWT_ALGORITHM=RS256
```

### 🔐 Generate Certificate for HTTPS (Optional)  

If `IS_SSL=TRUE` in your `.env`, generate the certificate files by running this file:  
```bash
./generate-certificate.sh
```

- **Notes**:  
  - On **Linux/macOS**: Run the script directly
  - On **Windows**: Use **WSL** to execute the `.sh` script

This will generate:
  - `./cert/mycert.key`
  - `./cert/mycert.cer`


Ensure these are correctly referenced in your `.env`:
```properties
IS_SSL=TRUE
SSL_KEYS=./cert/mycert.key
SSL_CERT=./cert/mycert.cer
```

### 👤 Create Dedicated PostgreSQL User (Recommended)

For security reasons, it's recommended to avoid using the default postgres superuser. Use the following SQL script to create a dedicated user (`appuser`) and assign permissions:

```sql
-- Create appuser and database
CREATE USER appuser WITH PASSWORD 'app@123';

-- Allow user to connect to database
GRANT CONNECT, TEMP, CREATE ON DATABASE payment_service TO appuser;

-- Grant permissions on public schema
GRANT USAGE, CREATE ON SCHEMA public TO appuser;

-- Grant all permissions on existing tables
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO appuser;

-- Grant all permissions on sequences (if using SERIAL/BIGSERIAL ids)
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO appuser;

-- Ensure future tables/sequences will be accessible too
ALTER DEFAULT PRIVILEGES IN SCHEMA public
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO appuser;

-- Ensure future sequences will be accessible too
ALTER DEFAULT PRIVILEGES IN SCHEMA public
GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO appuser;
```

Update your `.env` accordingly:
```properties
DB_USER=appuser
DB_PASS=app@123
```

---


## 🚀 Running the Application  

This section provides step-by-step instructions to run the application either **locally** or via **Docker containers**.

- **Notes**:  
  - All commands are defined in the `Makefile`.
  - To run using `make`, ensure that `make` is installed on your system.
  - To run the application in containers, make sure `Docker` is installed and running.

### 🧪 Run Unit Tests

```bash
make test
```

### 🔧 Run Locally (Non-containerized)

Ensure Redis and PostgreSQL are running locally, then:

```bash
make run
```

### 🐳 Run Using Docker

To build and run all services (Redis, PostgreSQL, Go app):

```bash
make docker-up
```

To stop and remove all containers:

```bash
make docker-down
```

- **Notes**:  
  - Before running the application inside Docker, make sure to update your environment variables `.env`
    - Change `DB_HOST=localhost` to `DB_HOST=idempotency-postgres`.
    - Change `REDIS_HOST=localhost` to `REDIS_HOST=idempotency-redis`.
    - Change `KAFKA_BROKERS=localhost:9092` to `KAFKA_BROKERS=idempotency-kafka:9092`.

### 🟢 Application is Running

Now your application is accessible at:
```bash
http://localhost:1000
```

or 

```bash
https://localhost:1000 (if SSL is enabled)
```

---

## 🧪 Testing Scenarios  

### 🔐 Login API

**Endpoint**: `POST https://localhost:1000/auth/login`

#### ✅ Scenario 1: Successful Login

**Request**:

```json
{
  "username": "admin",
  "password": "P@ssw0rd"
}
```

**Response**:

```json
{
  "message": "Login successful",
  "error": null,
  "path": "/auth/login",
  "status": 200,
  "data": {
    "accessToken": "<JWT>",
    "refreshToken": "<UUID>",
    "expirationDate": "2025-05-25T12:58:00Z",
    "tokenType": "Bearer"
  },
  "timestamp": "2025-05-23T12:58:00Z"
}
```

#### ❌ Scenario 2: Invalid Credentials

**Request with invalid user**:
```json
{
  "username": "invalid_user",
  "password": "P@ssw0rd"
}
```

**Response**:
```json
{
  "message": "Failed to login",
  "error": "user with the given username not found",
  "path": "/auth/login",
  "status": 401,
  "data": null,
  "timestamp": "2025-05-23T15:18:23Z"
}
```

**Request with invalid password**:
```json
{
  "username": "admin",
  "password": "invalid_password"
}
```

**Response**:
```json
{
    "message": "Failed to login",
    "error": "invalid password",
    "path": "/auth/login",
    "status": 401,
    "data": null,
    "timestamp": "2025-05-23T15:51:39.288150079Z"
}
```

#### 🚫 Scenario 3: Disabled User

Precondition:
```sql
UPDATE users SET is_enabled = false WHERE id = 2;
```

**Request**:
```json
{
  "username": "userone",
  "password": "P@ssw0rd"
}
```

**Response**:
```json
{
  "message": "Failed to login",
  "error": "user is not enabled",
  "path": "/auth/login",
  "status": 401,
  "data": null,
  "timestamp": "2025-05-23T15:19:24Z"
}
```

#### ⏳ Scenario 4: Rate Limit Exceeded on Login

Precondition:
  - The rate limiter is configured as:
    - **rate.Limit**: rate.Every(30 * time.Second)
    - **burst**: 1
    - **expireAfter**: 5 * time.Minute
  - **Artinya**: allow `1 request` every `30 seconds`, with a burst capacity of `1`, within a `5-minute` window

**Request**: repeated quickly using valid credentials

```json
{
    "username": "admin",
    "password": "P@ssw0rd"
}
```

  - Steps:
    - Send the request once → receive access token.
    - Send the same request again shortly after (before 30 seconds pass).

**Response will be**:
```json
{
    "message": "Rate limit exceeded",
    "error": "You have exceeded the rate limit. Please try again later.",
    "path": "/auth/login",
    "status": 429,
    "data": null,
    "timestamp": "2025-05-23T16:01:30.407871957Z"
}
```


### 🔄 Refresh Token API

**Endpoint**: `POST https://localhost:1000/auth/refresh-token`

#### ✅ Scenario 1: Successful Refresh Token

**Request**:
```json
{
  "refreshToken": "<valid_refresh_token>"
}
```

**Response**:
```json
{
  "message": "Token refreshed successfully",
  "error": null,
  "path": "/auth/refresh-token",
  "status": 200,
  "data": {
    "accessToken": "<JWT>",
    "refreshToken": "<new_UUID>",
    "expirationDate": "2025-05-25T15:23:51Z",
    "tokenType": "Bearer"
  },
  "timestamp": "2025-05-23T15:23:51Z"
}
```

#### ❌ Scenario 2: Invalid Refresh Token

**Request**:
```json
{
  "refreshToken": "<invalid_refresh_token>"
}
```

**Response**:
```json
{
  "message": "Failed to refresh token",
  "error": "record not found",
  "path": "/auth/refresh-token",
  "status": 401,
  "data": null,
  "timestamp": "2025-05-23T15:24:47Z"
}
```

#### 🔁 Scenario 3: Expired Refresh Token (Auto Regenerate)

**Request**:
```json
{
  "refreshToken": "<expired_refresh_token>"
}
```

**Response**:
```json
{
  "message": "Token refreshed successfully",
  "error": null,
  "path": "/auth/refresh-token",
  "status": 200,
  "data": {
    "accessToken": "<new_JWT>",
    "refreshToken": "<new_UUID>",
    "expirationDate": "2025-05-25T15:29:02Z",
    "tokenType": "Bearer"
  },
  "timestamp": "2025-05-23T15:29:02Z"
}
```

### 👨‍👩‍👧‍👦 Consumer API

All requests below must include a valid JWT token in the `Authorization` header:
```http
Authorization: Bearer <valid_token>
```

#### Scenario 1: Create Consumer

**Endpoint**: 
```http
POST https://localhost:1000/api/v1/consumers
```

**Request**:
```json
{
    "fullname": "Austin Libertus",
    "username": "auslibertus",
    "email": "austin.libertus@example.com",
    "phone": "+628997452753",
    "address": "Jl. Anggrek No. 4, Jakarta",
    "birthDate": "1990-03-05"
}
```

**Response**:
```json
{
    "message": "Consumer created successfully",
    "error": null,
    "path": "/api/v1/consumers",
    "status": 201,
    "data": {
        "id": "4c6c42bc-3b82-4f34-9eaf-c4dcfb246ec0",
        "fullname": "Austin Libertus",
        "username": "auslibertus",
        "email": "austin.libertus@example.com",
        "phone": "628997452753",
        "address": "Jl. Anggrek No. 4, Jakarta",
        "birthDate": "1990-03-05",
        "status": "inactive",
        "createdAt": "2025-06-18T11:42:13.165068Z",
        "updatedAt": "2025-06-18T11:42:13.165068Z"
    },
    "timestamp": "2025-06-18T11:42:13.171205664Z"
}
```

#### Scenario 2: Update Consumer Status

**Endpoint**: 
```http
PATCH https://localhost:1000/api/v1/consumers/4c6c42bc-3b82-4f34-9eaf-c4dcfb246ec0?status=active
```

**Response**:
```json
{
    "message": "Consumer status updated successfully",
    "error": null,
    "path": "/api/v1/consumers/4c6c42bc-3b82-4f34-9eaf-c4dcfb246ec0",
    "status": 200,
    "data": {
        "id": "4c6c42bc-3b82-4f34-9eaf-c4dcfb246ec0",
        "fullname": "Austin Libertus",
        "username": "auslibertus",
        "email": "austin.libertus@example.com",
        "phone": "628997452753",
        "address": "Jl. Anggrek No. 4, Jakarta",
        "birthDate": "1990-03-05",
        "status": "active",
        "createdAt": "2025-06-18T11:42:13.165068Z",
        "updatedAt": "2025-06-18T11:44:52.059458364Z"
    },
    "timestamp": "2025-06-18T11:44:52.062880937Z"
}
```

#### Scenario 3: Get All Consumers

**Endpoint**: 
```http
GET https://localhost:1000/api/v1/consumers?page=1&limit=10
```

**Response**:
```json
{
    "message": "All consumers retrieved successfully",
    "error": null,
    "path": "/api/v1/consumers",
    "status": 200,
    "data": [
        {
            "id": "74fe86f3-6324-42c2-97b4-fa3225461299",
            "fullname": "John Doe",
            "username": "johndoe",
            "email": "john.doe@example.com",
            "phone": "6281234567890",
            "address": "Jl. Merdeka No. 123, Jakarta",
            "birthDate": "1990-05-10",
            "status": "active",
            "createdAt": "2025-06-18T11:40:56.66591Z",
            "updatedAt": "2025-06-18T11:40:56.66591Z"
        }
        ...
    ],
    "timestamp": "2025-06-18T13:11:24.539972654Z"
}
```

### 💳 Transaction API

All requests below must include a valid JWT token in the `Authorization` header:
```http
Authorization: Bearer <valid_token>
```

Each `POST` request must also include a unique `Idempotency-Key` header to ensure safe retries:
```http
Idempotency-Key: <UUID>
```

#### ✅ Scenario 1: Create a New Transaction with Non-Existent Consumer

**Endpoint**:  
```http
POST https://localhost:1000/api/v1/transactions
```

**Request**:
```json
{
  "type": "payment",
  "amount": 150000.00,
  "consumerId": "4c6c42bc-3b82-4f34-9eaf-c4dcfb246ec0"
}
```

**Response**:
```json
{
  "message": "Consumer not found",
  "error": "No consumer found with the given ID",
  "path": "/api/v1/transactions",
  "status": 404,
  "data": null,
  "timestamp": "2025-06-18T16:02:57.380180455Z"
}
```

#### ✅ Scenario 2: Create a New Transaction with Inactive Consumer

**Endpoint**:  
```http
POST https://localhost:1000/api/v1/transactions
```

**Request**:
```json
{
  "type": "payment",
  "amount": 150000.00,
  "consumerId": "4c6c42bc-3b82-4f34-9eaf-c4dcfb246ec0"
}
```

**Response**:
```json
{
  "message": "Invalid transaction data",
  "error": "Transaction data is invalid, this could be due to missing required fields or incorrect data types",
  "path": "/api/v1/transactions",
  "status": 400,
  "data": null,
  "timestamp": "2025-06-18T16:03:23.349569947Z"
}
```

#### ✅ Scenario 3: Create a New Transaction Successfully

**Endpoint**:  
```http
POST https://localhost:1000/api/v1/transactions
```

**Request**:
```json
{
  "type": "payment",
  "amount": 150000.00,
  "consumerId": "a1b9d37e-2e7d-42b2-9d3e-7b492162905d"
}
```

**Response**:
```json
{
  "message": "Transaction created successfully",
  "error": null,
  "path": "/api/v1/transactions",
  "status": 201,
  "data": {
    "id": "147735b9-eff7-469d-ac85-3b8108825ce4",
    "idempotencyCacheKey": "06f14f72-dfba-49ca-aa4e-d85b532ca0b7",
    "type": "payment",
    "amount": 150000,
    "status": "pending",
    "consumerId": "a1b9d37e-2e7d-42b2-9d3e-7b492162905d",
    "createdAt": "2025-06-18T16:19:59.952804Z",
    "updatedAt": "2025-06-18T16:19:59.952804Z"
  },
  "timestamp": "2025-06-18T16:20:01.005272013Z"
}
```

#### ✅ Scenario 4: Same Idempotency-Key but Different Request

**Endpoint**:  
```http
POST https://localhost:1000/api/v1/transactions
```

**Request**:
```json
{
  "type": "payment",
  "amount": 170000.00,
  "consumerId": "a1b9d37e-2e7d-42b2-9d3e-7b492162905d"
}
```

**Response**:
```json
{
  "message": "Conflict",
  "error": "Request with the same Idempotency-Key but different body has already been processed",
  "path": "/api/v1/transactions",
  "status": 409,
  "data": null,
  "timestamp": "2025-06-18T15:24:50.515722414Z"
}
```

#### ✅ Scenario 5: Same Idempotency-Key and Same Request (Previously Failed)

**Endpoint**:  
```http
POST https://localhost:1000/api/v1/transactions
```

**Request**:
```json
{
  "type": "payment",
  "amount": 150000.00,
  "consumerId": "a1b9d37e-2e7d-42b2-9d3e-7b492162905d"
}
```

**Response**:
```json
{
  "message": "Request already processed",
  "error": null,
  "path": "/api/v1/transactions",
  "status": 200,
  "data": {
    "amount": 150000,
    "consumerId": "a1b9d37e-2e7d-42b2-9d3e-7b492162905d",
    "createdAt": "2025-06-18T16:19:59.952804Z",
    "id": "147735b9-eff7-469d-ac85-3b8108825ce4",
    "idempotencyCacheKey": "06f14f72-dfba-49ca-aa4e-d85b532ca0b7",
    "status": "failed",
    "type": "payment",
    "updatedAt": "2025-06-18T16:20:08.921759395Z"
  },
  "timestamp": "2025-06-18T16:21:03.791516931Z"
}
```
