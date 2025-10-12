# Deployment Guide

## Prerequisites

- Docker and Docker Compose installed
- PostgreSQL database (for non-Docker deployments)
- Domain name and SSL certificate (for production)
- Cloud provider account (AWS, GCP, Azure, etc.)

## Local Development

### Using Docker Compose

1. **Start the services:**

```bash
docker-compose up -d
```

2. **Check logs:**

```bash
docker-compose logs -f api
```

3. **Stop the services:**

```bash
docker-compose down
```

### Without Docker

1. **Set up PostgreSQL:**

```bash
# macOS
brew install postgresql@15
brew services start postgresql@15

# Ubuntu/Debian
sudo apt-get install postgresql-15
sudo systemctl start postgresql
```

2. **Create database:**

```bash
psql -U postgres
CREATE DATABASE event_ticketing;
\q
```

3. **Update .env file:**

```env
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=your_password
DB_NAME=event_ticketing
```

4. **Run the application:**

```bash
go run cmd/api/main.go
```

## Staging Deployment

### Using Docker Compose

1. **Create .env.staging file:**

```env
APP_ENV=staging
DB_HOST=staging-db.example.com
DB_PORT=5432
DB_USER=staging_user
DB_PASSWORD=secure_password
DB_NAME=event_ticketing_staging
DB_SSLMODE=require
```

2. **Deploy:**

```bash
docker-compose -f docker-compose.staging.yml up -d
```

### Using Cloud Providers

#### AWS ECS (Elastic Container Service)

1. **Build and push Docker image:**

```bash
# Authenticate with ECR
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin <account-id>.dkr.ecr.us-east-1.amazonaws.com

# Build image
docker build -t event-ticketing-api .

# Tag image
docker tag event-ticketing-api:latest <account-id>.dkr.ecr.us-east-1.amazonaws.com/event-ticketing-api:latest

# Push image
docker push <account-id>.dkr.ecr.us-east-1.amazonaws.com/event-ticketing-api:latest
```

2. **Create ECS task definition:**

```json
{
  "family": "event-ticketing-api",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "512",
  "memory": "1024",
  "containerDefinitions": [
    {
      "name": "api",
      "image": "<account-id>.dkr.ecr.us-east-1.amazonaws.com/event-ticketing-api:latest",
      "portMappings": [
        {
          "containerPort": 8080,
          "protocol": "tcp"
        }
      ],
      "environment": [
        {
          "name": "APP_ENV",
          "value": "staging"
        }
      ],
      "secrets": [
        {
          "name": "DB_PASSWORD",
          "valueFrom": "arn:aws:secretsmanager:us-east-1:123456789:secret:db-password"
        }
      ]
    }
  ]
}
```

3. **Create ECS service:**

```bash
aws ecs create-service \
  --cluster staging-cluster \
  --service-name event-ticketing-api \
  --task-definition event-ticketing-api \
  --desired-count 2 \
  --launch-type FARGATE \
  --network-configuration "awsvpcConfiguration={subnets=[subnet-xxx],securityGroups=[sg-xxx],assignPublicIp=ENABLED}"
```

## Production Deployment

### Kubernetes Deployment

1. **Create namespace:**

```bash
kubectl create namespace event-ticketing
```

2. **Create secrets:**

```bash
kubectl create secret generic db-credentials \
  --from-literal=username=prod_user \
  --from-literal=password=super_secure_password \
  -n event-ticketing
```

3. **Apply deployment configuration:**

**deployment.yaml:**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: event-ticketing-api
  namespace: event-ticketing
spec:
  replicas: 3
  selector:
    matchLabels:
      app: event-ticketing-api
  template:
    metadata:
      labels:
        app: event-ticketing-api
    spec:
      containers:
        - name: api
          image: your-registry/event-ticketing-api:latest
          ports:
            - containerPort: 8080
          env:
            - name: APP_ENV
              value: "production"
            - name: DB_HOST
              value: "postgres-service"
            - name: DB_PORT
              value: "5432"
            - name: DB_NAME
              value: "event_ticketing"
            - name: DB_USER
              valueFrom:
                secretKeyRef:
                  name: db-credentials
                  key: username
            - name: DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: db-credentials
                  key: password
          resources:
            requests:
              memory: "256Mi"
              cpu: "250m"
            limits:
              memory: "512Mi"
              cpu: "500m"
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 30
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
```

**service.yaml:**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: event-ticketing-api-service
  namespace: event-ticketing
spec:
  type: LoadBalancer
  ports:
    - port: 80
      targetPort: 8080
      protocol: TCP
  selector:
    app: event-ticketing-api
```

4. **Apply configurations:**

```bash
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
```

### Using Docker Swarm

1. **Initialize swarm:**

```bash
docker swarm init
```

2. **Create overlay network:**

```bash
docker network create --driver overlay event-ticketing-network
```

3. **Deploy stack:**

```bash
docker stack deploy -c docker-compose.prod.yml event-ticketing
```

4. **Check services:**

```bash
docker service ls
docker service logs event-ticketing_api
```

## Database Migration

### Manual Migration

```bash
# Connect to the container
docker exec -it event_ticketing_api sh

# Run migrations (automatically runs on startup)
./main
```

### Using Flyway or Migrate

```bash
# Install golang-migrate
brew install golang-migrate

# Create migration
migrate create -ext sql -dir migrations -seq create_events_table

# Run migrations
migrate -path migrations -database "postgresql://user:password@localhost:5432/event_ticketing?sslmode=disable" up
```

## Monitoring and Logging

### Add Prometheus Metrics

```go
// Add to main.go
import "github.com/prometheus/client_golang/prometheus/promhttp"

// Add metrics endpoint
router.GET("/metrics", gin.WrapH(promhttp.Handler()))
```

### Configure Logging

Use structured logging with tools like:

- **Logrus**: Structured logging
- **Zap**: High-performance logging
- **ELK Stack**: Centralized logging
- **CloudWatch**: AWS logging

### Health Checks

Monitor these endpoints:

- `GET /health` - API health
- `GET /health/db` - Database connectivity

## SSL/TLS Configuration

### Using Let's Encrypt

```bash
# Install certbot
sudo apt-get install certbot

# Get certificate
sudo certbot certonly --standalone -d api.yourdomain.com

# Configure nginx
server {
    listen 443 ssl;
    server_name api.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/api.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/api.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://localhost:8080;
    }
}
```

## Backup and Recovery

### Database Backup

```bash
# Backup
pg_dump -U postgres -h localhost event_ticketing > backup.sql

# Restore
psql -U postgres -h localhost event_ticketing < backup.sql

# Automated backup script
#!/bin/bash
DATE=$(date +%Y%m%d_%H%M%S)
pg_dump -U postgres event_ticketing | gzip > /backups/db_$DATE.sql.gz
find /backups -name "db_*.sql.gz" -mtime +7 -delete
```

### Container Backup

```bash
# Backup volumes
docker run --rm -v event_ticketing_postgres_data:/data -v $(pwd):/backup alpine tar czf /backup/postgres-backup.tar.gz /data
```

## Rollback Strategy

1. **Keep previous versions:**

```bash
docker tag event-ticketing-api:latest event-ticketing-api:v1.0.0
```

2. **Quick rollback:**

```bash
docker service update --image event-ticketing-api:v1.0.0 event-ticketing_api
```

## Performance Tuning

1. **Database connection pooling:**

   - Max open connections: 100
   - Max idle connections: 10

2. **Server timeouts:**

   - Read timeout: 30s
   - Write timeout: 30s
   - Idle timeout: 60s

3. **Load balancing:**

   - Use HAProxy, Nginx, or cloud load balancers
   - Configure health checks

4. **Caching:**
   - Implement Redis for frequently accessed data
   - Use CDN for static content

## Security Checklist

- [ ] Use environment variables for secrets
- [ ] Enable SSL/TLS
- [ ] Implement rate limiting
- [ ] Add authentication/authorization
- [ ] Use security headers
- [ ] Regular security updates
- [ ] Database encryption at rest
- [ ] Network security groups
- [ ] Regular backups
- [ ] Monitoring and alerting

## Troubleshooting

### Application won't start

```bash
# Check logs
docker logs event_ticketing_api

# Check database connection
docker exec -it event_ticketing_api sh
ping postgres
```

### Database connection issues

```bash
# Test connection
psql -h localhost -U postgres -d event_ticketing

# Check PostgreSQL logs
docker logs event_ticketing_db
```

### High memory usage

```bash
# Check container stats
docker stats event_ticketing_api

# Adjust memory limits in docker-compose.yml
```

## Support

For deployment issues, please check:

1. Application logs
2. Database logs
3. System resources
4. Network connectivity
5. Environment variables
