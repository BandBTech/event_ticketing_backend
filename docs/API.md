# API Documentation

## Overview

The Event Ticketing API is a RESTful API that provides endpoints for managing events. All responses follow a standard JSON format.

## Base URL

- **Local**: `http://localhost:8080`
- **Staging**: `https://staging-api.example.com`
- **Production**: `https://api.example.com`

## Standard Response Format

### Success Response

```json
{
  "success": true,
  "message": "Operation successful",
  "data": {}
}
```

### Error Response

```json
{
  "success": false,
  "message": "Operation failed",
  "error": "Detailed error message"
}
```

## Authentication

_Authentication is not yet implemented. Future versions will include JWT-based authentication._

## Rate Limiting

_Rate limiting is not yet implemented. Future versions will include rate limiting based on IP address or API key._

## Error Codes

| Status Code | Description                                           |
| ----------- | ----------------------------------------------------- |
| 200         | OK - Request successful                               |
| 201         | Created - Resource created successfully               |
| 400         | Bad Request - Invalid input                           |
| 404         | Not Found - Resource not found                        |
| 500         | Internal Server Error - Server error                  |
| 503         | Service Unavailable - Service temporarily unavailable |

## Endpoints

### Health Checks

#### Check API Health

```
GET /health
```

**Response:**

```json
{
  "success": true,
  "message": "API is healthy",
  "data": {
    "status": "up"
  }
}
```

#### Check Database Health

```
GET /health/db
```

**Response:**

```json
{
  "success": true,
  "message": "Database is healthy",
  "data": {
    "status": "up"
  }
}
```

### Events API

#### Create Event

```
POST /api/v1/events
```

**Request Body:**

```json
{
  "title": "Tech Conference 2024",
  "description": "Annual technology conference",
  "location": "San Francisco, CA",
  "start_date": "2024-06-15T09:00:00Z",
  "end_date": "2024-06-17T18:00:00Z",
  "price": 299.99,
  "capacity": 500
}
```

**Response (201):**

```json
{
  "success": true,
  "message": "Event created successfully",
  "data": {
    "id": 1,
    "title": "Tech Conference 2024",
    "description": "Annual technology conference",
    "location": "San Francisco, CA",
    "start_date": "2024-06-15T09:00:00Z",
    "end_date": "2024-06-17T18:00:00Z",
    "price": 299.99,
    "capacity": 500,
    "available": 500,
    "status": "active",
    "created_at": "2024-01-15T10:30:00Z",
    "updated_at": "2024-01-15T10:30:00Z"
  }
}
```

#### Get All Events

```
GET /api/v1/events
```

**Response (200):**

```json
{
  "success": true,
  "message": "Events fetched successfully",
  "data": [
    {
      "id": 1,
      "title": "Tech Conference 2024",
      "description": "Annual technology conference",
      "location": "San Francisco, CA",
      "start_date": "2024-06-15T09:00:00Z",
      "end_date": "2024-06-17T18:00:00Z",
      "price": 299.99,
      "capacity": 500,
      "available": 500,
      "status": "active",
      "created_at": "2024-01-15T10:30:00Z",
      "updated_at": "2024-01-15T10:30:00Z"
    }
  ]
}
```

#### Get Event by ID

```
GET /api/v1/events/:id
```

**Parameters:**

- `id` (path) - Event ID

**Response (200):**

```json
{
  "success": true,
  "message": "Event fetched successfully",
  "data": {
    "id": 1,
    "title": "Tech Conference 2024",
    "description": "Annual technology conference",
    "location": "San Francisco, CA",
    "start_date": "2024-06-15T09:00:00Z",
    "end_date": "2024-06-17T18:00:00Z",
    "price": 299.99,
    "capacity": 500,
    "available": 500,
    "status": "active",
    "created_at": "2024-01-15T10:30:00Z",
    "updated_at": "2024-01-15T10:30:00Z"
  }
}
```

#### Update Event

```
PUT /api/v1/events/:id
```

**Parameters:**

- `id` (path) - Event ID

**Request Body:** (all fields optional)

```json
{
  "title": "Updated Tech Conference 2024",
  "description": "Updated description",
  "location": "Los Angeles, CA",
  "start_date": "2024-06-20T09:00:00Z",
  "end_date": "2024-06-22T18:00:00Z",
  "price": 349.99,
  "capacity": 600,
  "status": "active"
}
```

**Response (200):**

```json
{
  "success": true,
  "message": "Event updated successfully",
  "data": {
    "id": 1,
    "title": "Updated Tech Conference 2024",
    "description": "Updated description",
    "location": "Los Angeles, CA",
    "start_date": "2024-06-20T09:00:00Z",
    "end_date": "2024-06-22T18:00:00Z",
    "price": 349.99,
    "capacity": 600,
    "available": 500,
    "status": "active",
    "created_at": "2024-01-15T10:30:00Z",
    "updated_at": "2024-01-15T11:45:00Z"
  }
}
```

#### Delete Event

```
DELETE /api/v1/events/:id
```

**Parameters:**

- `id` (path) - Event ID

**Response (200):**

```json
{
  "success": true,
  "message": "Event deleted successfully"
}
```

## Swagger Documentation

Interactive API documentation is available at:

```
http://localhost:8080/swagger/index.html
```

## Examples with cURL

### Create an Event

```bash
curl -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Music Festival",
    "description": "Summer music festival",
    "location": "Austin, TX",
    "start_date": "2024-07-01T12:00:00Z",
    "end_date": "2024-07-03T23:00:00Z",
    "price": 199.99,
    "capacity": 1000
  }'
```

### Get All Events

```bash
curl -X GET http://localhost:8080/api/v1/events
```

### Get Event by ID

```bash
curl -X GET http://localhost:8080/api/v1/events/1
```

### Update Event

```bash
curl -X PUT http://localhost:8080/api/v1/events/1 \
  -H "Content-Type: application/json" \
  -d '{
    "price": 249.99,
    "capacity": 1200
  }'
```

### Delete Event

```bash
curl -X DELETE http://localhost:8080/api/v1/events/1
```

## Future Enhancements

- User authentication and authorization
- Ticket booking system
- Payment integration
- Email notifications
- Search and filtering
- Pagination
- Rate limiting
- Caching
- Real-time updates via WebSockets
