# Event Status Bug Fix - March 30, 2026

## Problem Found

Your events show `status = "sales_end"` even though their tier **sales haven't started yet**:

```
Event: "maha mela"
Current Status:  sales_end
Sales Start:     2026-03-30 04:22 UTC
Sales End:       2026-03-31 04:23 UTC
NOW:             2026-03-30 ~23:13+ UTC
                 ↓
            Should be: on_sale ✅
            Actually is: sales_end ❌
```

---

## Root Cause

The `EventStatusWorker.updateTierBasedSalesStatus()` function had **incomplete logic**:

**Before Fix:**

```go
anyTierActive := false
allTiersEnded := true

// Loop through tiers...
// Missing: check if we're BEFORE all tiers start

if anyTierActive {
    targetStatus = "on_sale"
} else if allTiersEnded {
    targetStatus = "sales_end"  ← Default to this without checking if tiers haven't started
}
```

**Problem:**

- Events with `status = "sales_end"` weren't being checked if tiers were actually active
- Worker only checked if `anyTierActive` or `allTiersEnded`
- **Missing:** check if we're BEFORE all tiers start (should be `"scheduled"`)

---

## Fixes Applied

### Fix #1: Added State Detection

```go
allTiersNotStarted := true

for _, tier := range event.Tiers {
    if tier.SalesStart != nil && tier.SalesEnd != nil {
        // Track if ANY tier has started
        if now.After(*tier.SalesStart) {
            allTiersNotStarted = false
        }
        // ... rest of logic
    }
}
```

### Fix #2: Explicit Status Transition Logic

```go
if anyTierActive {
    targetStatus = "on_sale"  // Between start and end
} else if allTiersEnded {
    targetStatus = "sales_end"  // After all tiers end
} else if allTiersNotStarted {
    targetStatus = "scheduled"  // BEFORE any tier starts ← NEW!
} else {
    targetStatus = event.Status  // Mixed state - keep current
}
```

### Fix #3: Enhanced Logging

```go
log.Printf("[EventStatusWorker] Event %s: Tier is ACTIVE → on_sale", event.ID)
// or
log.Printf("[EventStatusWorker] Event %s: All tiers NOT YET STARTED → scheduled", event.ID)
```

---

## How to Apply & Test

### Step 1: Deploy the Code Fix

```bash
cd /Users/aashbinsunar/Desktop/bandbtech/timro_ticket_system/event_ticketing_backend
go build ./cmd/api
docker-compose up -d  # or your deployment method
```

### Step 2: Run Diagnostic (Optional)

```bash
chmod +x diagnose_event_status.sh
./diagnose_event_status.sh
```

Shows:

- Current tier dates
- Status transition history
- What status events SHOULD have

### Step 3: Fix Existing Events (Quick SQL)

```bash
chmod +x fix_event_status.sh
./fix_event_status.sh
```

Or manually:

```sql
-- Fix events that are on_sale during sales period but show sales_end
UPDATE events e
SET status = 'on_sale'
FROM event_tiers t
WHERE e.id = t.event_id
  AND e.status = 'sales_end'
  AND e.deleted_at IS NULL
  AND t.deleted_at IS NULL
  AND NOW() BETWEEN t.sales_start AND t.sales_end;

-- Fix events before sales start but showing sales_end
UPDATE events e
SET status = 'scheduled'
WHERE e.status = 'sales_end'
  AND e.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM event_tiers t
    WHERE t.event_id = e.id
      AND t.deleted_at IS NULL
      AND t.sales_end IS NOT NULL
      AND NOW() < t.sales_end
  );
```

---

## Expected Behavior After Fix

**Timeline for single-tier event:**

```
Before sales_start:     status = "scheduled" ✅
Between start/end:      status = "on_sale" ✅
After sales_end:        status = "sales_end" ✅
```

**Your events:**

- Event "maha mela":
  - Before: `sales_end` ❌
  - After: `on_sale` ✅ (since we're between 04:22 March 30 and 04:23 March 31)
- Event "new 123":
  - Before: `sales_end` ❌
  - After: `scheduled` ✅ (sales start July 20, 2026 - far future)

---

## Monitoring

The worker runs these checks:

- **Every 5 minutes:** Basic scheduled → sales transitions
- **Every 1 minute:** Tier-based active status transitions (on_sale ↔ sales_end)

Check logs for:

```bash
docker-compose logs event_ticketing_api | grep EventStatusWorker
```

Expected output after fix:

```
[EventStatusWorker] ✅ Updated event maha mela from sales_end to on_sale
[EventStatusWorker] ✅ Updated event new_123 from sales_end to scheduled
```

---

## Files Modified

1. **internal/workers/event_status_worker.go**
   - Enhanced `updateTierBasedSalesStatus()` with:
     - `allTiersNotStarted` state detection
     - Explicit handling for "before all tiers start"
     - Better debug logging with emojis

---

## Summary

| Status     | Issue                                         | Fix                                        |
| ---------- | --------------------------------------------- | ------------------------------------------ |
| sales_end  | Shows for events before sales start           | ✅ Now checks if tiers haven't started yet |
| sales_end  | Doesn't auto-correct when tiers become active | ✅ Enhanced logic runs every 1 min         |
| No logging | Hard to debug why status changed              | ✅ Added detailed logging with state info  |

**Result:** Events now show correct status based on tier sales dates automatically! 🎉
