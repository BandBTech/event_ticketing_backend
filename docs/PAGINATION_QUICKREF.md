# Quick Reference: Pagination Single Source of Truth

## 🎯 One File to Rule Them All

**File**: `pkg/utils/handlers.go`

All pagination logic lives here. Changes made in this file affect the **entire backend**.

---

## 📋 Common Tasks

### Change Default Page Size

```go
// In pkg/utils/handlers.go
var DefaultPaginationConfig = PaginationConfig{
    DefaultLimit: 10,   // ← Change this number
    MaxLimit:     100,
}
```

### Change Maximum Allowed Limit

```go
// In pkg/utils/handlers.go
var DefaultPaginationConfig = PaginationConfig{
    DefaultLimit: 10,
    MaxLimit:     100,  // ← Change this number
}
```

### Add New Field to Pagination Response

```go
// In pkg/utils/handlers.go - BuildPaginatedResponse()
return map[string]interface{}{
    "data":        data,
    "total":       total,
    "page":        page,
    "limit":       limit,
    "total_pages": totalPages,
    "new_field":   value,  // ← Add here
}
```

---

## 💻 Usage Pattern (Copy & Paste)

```go
func (h *Handler) GetItems(c *gin.Context) {
    // 1. Extract pagination (specify default limit for this endpoint)
    pagination := utils.GetPaginationParams(c, 10)

    // 2. Use in service call
    items, total, err := h.service.GetItems(pagination.Page, pagination.Limit)
    if err != nil {
        utils.HandleError(c, err)
        return
    }

    // 3. Build response
    response := utils.BuildPaginatedResponse(items, total, pagination.Page, pagination.Limit)

    // 4. Send response
    utils.SuccessResponse(c, http.StatusOK, "Items retrieved successfully", response)
}
```

---

## ✅ Verified Files Using Pagination Utils

All these files now use centralized pagination:

- ✅ `event_handler.go`
- ✅ `ticket_handler.go`
- ✅ `financial_handler.go`
- ✅ `organizer_user_handler.go`
- ✅ `public_handler.go`
- ✅ `auth_handler.go`

---

## 🚫 What NOT to Do

❌ Don't write manual pagination:

```go
// DON'T DO THIS
page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
if page < 1 { page = 1 }
if limit < 1 || limit > 100 { limit = 10 }
```

✅ Use the utility instead:

```go
// DO THIS
pagination := utils.GetPaginationParams(c, 10)
```

❌ Don't calculate total_pages manually:

```go
// DON'T DO THIS
totalPages := (total + int64(limit) - 1) / int64(limit)
response := map[string]interface{}{
    "total": total,
    "page": page,
    "limit": limit,
    "total_pages": totalPages,
}
```

✅ Use BuildPaginatedResponse:

```go
// DO THIS
response := utils.BuildPaginatedResponse(data, total, pagination.Page, pagination.Limit)
```

---

## 🧪 Testing Changes

After modifying `pkg/utils/handlers.go`:

```bash
# Build to verify no errors
go build ./cmd/api

# Test specific endpoint
curl "http://localhost:8080/api/v1/events?page=2&limit=20"
```

---

## 📚 Full Documentation

See `docs/PAGINATION_GUIDE.md` for complete details.

---

**Remember**: ONE file (`pkg/utils/handlers.go`) controls ALL pagination!
