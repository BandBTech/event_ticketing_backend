-- Event cancellation request workflow
CREATE TABLE IF NOT EXISTS "public"."event_cancellation_requests" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "event_id" uuid NOT NULL,
  "organizer_id" uuid NOT NULL,
  "reason" text NOT NULL,
  "status" character varying(20) NOT NULL DEFAULT 'pending',
  "admin_remark" text NULL,
  "reviewed_by" uuid NULL,
  "reviewed_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_event_cancellation_requests_event_id" ON "public"."event_cancellation_requests" ("event_id");
CREATE INDEX IF NOT EXISTS "idx_event_cancellation_requests_organizer_id" ON "public"."event_cancellation_requests" ("organizer_id");
CREATE INDEX IF NOT EXISTS "idx_event_cancellation_requests_status" ON "public"."event_cancellation_requests" ("status");
CREATE INDEX IF NOT EXISTS "idx_event_cancellation_requests_reviewed_by" ON "public"."event_cancellation_requests" ("reviewed_by");
CREATE INDEX IF NOT EXISTS "idx_event_cancellation_requests_deleted_at" ON "public"."event_cancellation_requests" ("deleted_at");

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_event_cancellation_requests_event'
  ) THEN
    ALTER TABLE "public"."event_cancellation_requests"
      ADD CONSTRAINT "fk_event_cancellation_requests_event"
      FOREIGN KEY ("event_id") REFERENCES "public"."events"("id") ON DELETE CASCADE;
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_event_cancellation_requests_organizer'
  ) THEN
    ALTER TABLE "public"."event_cancellation_requests"
      ADD CONSTRAINT "fk_event_cancellation_requests_organizer"
      FOREIGN KEY ("organizer_id") REFERENCES "public"."users"("id");
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_event_cancellation_requests_reviewer'
  ) THEN
    ALTER TABLE "public"."event_cancellation_requests"
      ADD CONSTRAINT "fk_event_cancellation_requests_reviewer"
      FOREIGN KEY ("reviewed_by") REFERENCES "public"."users"("id");
  END IF;
END $$;

-- Refund architecture extensions (transaction-level + idempotency metadata)
ALTER TABLE "public"."refunds" ADD COLUMN IF NOT EXISTS "order_id" text NULL;
ALTER TABLE "public"."refunds" ADD COLUMN IF NOT EXISTS "user_id" uuid NULL;
ALTER TABLE "public"."refunds" ADD COLUMN IF NOT EXISTS "payment_provider" text NULL;
ALTER TABLE "public"."refunds" ADD COLUMN IF NOT EXISTS "refund_type" character varying(50) NOT NULL DEFAULT 'ticket_refund';
ALTER TABLE "public"."refunds" ADD COLUMN IF NOT EXISTS "error_message" text NULL;
ALTER TABLE "public"."refunds" ADD COLUMN IF NOT EXISTS "retry_count" integer NOT NULL DEFAULT 0;
ALTER TABLE "public"."refunds" ADD COLUMN IF NOT EXISTS "processed_at" timestamptz NULL;

CREATE INDEX IF NOT EXISTS "idx_refunds_order_id" ON "public"."refunds" ("order_id");
CREATE INDEX IF NOT EXISTS "idx_refunds_user_id" ON "public"."refunds" ("user_id");
CREATE INDEX IF NOT EXISTS "idx_refunds_payment_provider" ON "public"."refunds" ("payment_provider");
CREATE INDEX IF NOT EXISTS "idx_refunds_refund_type" ON "public"."refunds" ("refund_type");
CREATE INDEX IF NOT EXISTS "idx_refunds_processed_at" ON "public"."refunds" ("processed_at");

CREATE UNIQUE INDEX IF NOT EXISTS "idx_refund_dedupe" ON "public"."refunds" ("event_id", "transaction_id", "refund_type", "ticket_id");

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'fk_refunds_user'
  ) THEN
    ALTER TABLE "public"."refunds"
      ADD CONSTRAINT "fk_refunds_user"
      FOREIGN KEY ("user_id") REFERENCES "public"."users"("id");
  END IF;
END $$;
