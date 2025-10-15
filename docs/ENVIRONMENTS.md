# Environment Management Guide

## Overview

This project supports multiple environments with manual environment file selection for better security and control.

## Available Environments

### 🔧 Local Development

- **File**: `.env`
- **Purpose**: Docker-based local development (used automatically by docker-compose)
- **Features**: Debug logging, CORS disabled, rate limiting off

### 🧪 Staging

- **File**: `.env.staging`
- **Purpose**: Testing and QA environment
- **Features**: Info logging, moderate rate limiting, staging database

### 🚀 Production

- **File**: `.env.production`
- **Purpose**: Live production environment
- **Features**: Warn logging, strict rate limiting, production database

## Quick Start

### Using Deployment Script (Recommended)

```bash
# Local development
./deploy.sh local

# Staging deployment
./deploy.sh staging

# Production deployment (with confirmation)
./deploy.sh production

# View logs
./deploy.sh logs local
./deploy.sh logs staging
./deploy.sh logs production

# Stop all environments
./deploy.sh stop
```

### Docker Compose Commands

```bash
# Deploy the application
docker-compose up -d --build

# Stop the application
docker-compose down

# View logs
docker-compose logs -f api
```

## Environment Files

| File           | Purpose                   | Usage                                        |
| -------------- | ------------------------- | -------------------------------------------- |
| `.env`         | Application configuration | Contains all necessary environment variables |
| `.env.example` | Template                  | Copy to create new env files                 |

## Security Notes

⚠️ **Important**:

- Never commit `.env.production` with real credentials
- Use secrets management in production (AWS Secrets Manager, Vault, etc.)
- The Dockerfile doesn't copy any .env files - they're provided at runtime
- Environment variables are injected via docker-compose `--env-file` flag

## Configuration Differences

| Setting       | Local      | Staging    | Production |
| ------------- | ---------- | ---------- | ---------- |
| Log Level     | debug      | info       | warn       |
| Log Format    | text       | json       | json       |
| Rate Limiting | disabled   | 200/min    | 60/min     |
| CORS          | permissive | restricted | strict     |
| SSL           | disabled   | required   | required   |
| Host Binding  | 0.0.0.0    | 0.0.0.0    | 0.0.0.0    |

## Access Points

### Local Development

- **API**: http://localhost:8080
- **Swagger**: http://localhost:8080/api/docs
- **Health**: http://localhost:8080/api/v1/health

### Staging/Production

- Configure reverse proxy (Nginx) to handle HTTPS and domain routing
- API accessible through your configured domain
- Use proper SSL certificates (Let's Encrypt, etc.)

## Database & Redis

### Local

- PostgreSQL and Redis run in Docker containers
- Data persisted in Docker volumes
- No authentication required

### Staging/Production

- Use managed database services (AWS RDS, DigitalOcean, etc.)
- Enable SSL connections
- Use strong passwords and proper authentication
- Configure backups and monitoring

```

```
