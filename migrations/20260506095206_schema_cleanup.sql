-- Create "payment_attempts" table
CREATE TABLE "public"."payment_attempts" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "payment_intent_id" uuid NOT NULL,
  "provider" text NOT NULL,
  "provider_reference_id" text NULL,
  "provider_session_id" text NULL,
  "provider_charge_id" text NULL,
  "amount" bigint NOT NULL,
  "currency" text NOT NULL,
  "status" text NOT NULL,
  "payment_method_type" text NULL,
  "provider_data" jsonb NULL,
  "failure_reason" text NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_payment_attempts_payment_intent_id" to table: "payment_attempts"
CREATE INDEX "idx_payment_attempts_payment_intent_id" ON "public"."payment_attempts" ("payment_intent_id");
-- Create index "idx_payment_attempts_provider" to table: "payment_attempts"
CREATE INDEX "idx_payment_attempts_provider" ON "public"."payment_attempts" ("provider");
-- Create index "idx_payment_attempts_status" to table: "payment_attempts"
CREATE INDEX "idx_payment_attempts_status" ON "public"."payment_attempts" ("status");
-- Create "payment_audit_logs" table
CREATE TABLE "public"."payment_audit_logs" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "action" character varying(100) NOT NULL,
  "entity_type" character varying(50) NOT NULL,
  "entity_id" uuid NOT NULL,
  "actor_id" uuid NULL,
  "actor_type" character varying(50) NULL,
  "event_id" uuid NULL,
  "changes_before" jsonb NULL,
  "changes_after" jsonb NULL,
  "ip_address" character varying(45) NULL,
  "user_agent" text NULL,
  "metadata" jsonb NULL,
  "timestamp" timestamptz NOT NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_payment_audit_logs_action" to table: "payment_audit_logs"
CREATE INDEX "idx_payment_audit_logs_action" ON "public"."payment_audit_logs" ("action");
-- Create index "idx_payment_audit_logs_actor_id" to table: "payment_audit_logs"
CREATE INDEX "idx_payment_audit_logs_actor_id" ON "public"."payment_audit_logs" ("actor_id");
-- Create index "idx_payment_audit_logs_entity_id" to table: "payment_audit_logs"
CREATE INDEX "idx_payment_audit_logs_entity_id" ON "public"."payment_audit_logs" ("entity_id");
-- Create index "idx_payment_audit_logs_entity_type" to table: "payment_audit_logs"
CREATE INDEX "idx_payment_audit_logs_entity_type" ON "public"."payment_audit_logs" ("entity_type");
-- Create index "idx_payment_audit_logs_timestamp" to table: "payment_audit_logs"
CREATE INDEX "idx_payment_audit_logs_timestamp" ON "public"."payment_audit_logs" ("timestamp");
-- Create "payment_bills" table
CREATE TABLE "public"."payment_bills" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "bill_number" character varying(50) NOT NULL,
  "event_id" uuid NOT NULL,
  "organizer_id" uuid NOT NULL,
  "admin_id" uuid NOT NULL,
  "payment_method" text NULL,
  "payment_ref" text NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "notes" text NULL,
  "bill_date" timestamptz NULL,
  "paid_date" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "total_revenue" numeric NOT NULL DEFAULT 0,
  "total_commission" numeric NOT NULL DEFAULT 0,
  "organizer_earnings" numeric NOT NULL DEFAULT 0,
  "billed_amount" numeric NOT NULL DEFAULT 0,
  "paid_amount" numeric NOT NULL DEFAULT 0,
  "remaining_amount" numeric NOT NULL DEFAULT 0,
  "bill_type" text NOT NULL DEFAULT 'auto_calculated',
  "priority" text NOT NULL DEFAULT 'normal',
  "due_date" timestamptz NULL,
  "payment_screenshot_url" character varying(500) NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_payment_bills_bill_number" UNIQUE ("bill_number")
);
-- Create index "idx_payment_bills_admin_id" to table: "payment_bills"
CREATE INDEX "idx_payment_bills_admin_id" ON "public"."payment_bills" ("admin_id");
-- Create index "idx_payment_bills_deleted_at" to table: "payment_bills"
CREATE INDEX "idx_payment_bills_deleted_at" ON "public"."payment_bills" ("deleted_at");
-- Create index "idx_payment_bills_event_id" to table: "payment_bills"
CREATE INDEX "idx_payment_bills_event_id" ON "public"."payment_bills" ("event_id");
-- Create index "idx_payment_bills_organizer_id" to table: "payment_bills"
CREATE INDEX "idx_payment_bills_organizer_id" ON "public"."payment_bills" ("organizer_id");
-- Create "currency_rates" table
CREATE TABLE "public"."currency_rates" (
  "id" serial NOT NULL,
  "base_currency" character varying(3) NOT NULL,
  "target_currency" character varying(3) NOT NULL,
  "rate" numeric(20,10) NOT NULL,
  "source" character varying(50) NOT NULL DEFAULT 'exchangerate.host',
  "valid_from" timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "valid_until" timestamptz NULL,
  "is_active" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "currency_rates_base_currency_target_currency_valid_from_key" UNIQUE ("base_currency", "target_currency", "valid_from")
);
-- Create index "idx_currency_rates_active" to table: "currency_rates"
CREATE INDEX "idx_currency_rates_active" ON "public"."currency_rates" ("is_active", "valid_from" DESC);
-- Create index "idx_currency_rates_base_target" to table: "currency_rates"
CREATE INDEX "idx_currency_rates_base_target" ON "public"."currency_rates" ("base_currency", "target_currency");
-- Create index "idx_currency_rates_valid_from" to table: "currency_rates"
CREATE INDEX "idx_currency_rates_valid_from" ON "public"."currency_rates" ("valid_from" DESC);
-- Create index "idx_currency_rates_valid_until" to table: "currency_rates"
CREATE INDEX "idx_currency_rates_valid_until" ON "public"."currency_rates" ("valid_until");
-- Create "dead_letter_queues" table
CREATE TABLE "public"."dead_letter_queues" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "task_type" character varying(100) NOT NULL,
  "payload" jsonb NULL,
  "error" text NULL,
  "retries" bigint NULL DEFAULT 0,
  "max_retry" bigint NULL DEFAULT 5,
  "status" character varying(20) NOT NULL DEFAULT 'pending',
  "failed_at" timestamptz NOT NULL,
  "resolved_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "stripe_event_id" character varying(255) NULL,
  "request_id" character varying(255) NULL,
  "metadata" jsonb NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_dead_letter_queues_request_id" to table: "dead_letter_queues"
CREATE INDEX "idx_dead_letter_queues_request_id" ON "public"."dead_letter_queues" ("request_id");
-- Create index "idx_dead_letter_queues_status" to table: "dead_letter_queues"
CREATE INDEX "idx_dead_letter_queues_status" ON "public"."dead_letter_queues" ("status");
-- Create index "idx_dead_letter_queues_stripe_event_id" to table: "dead_letter_queues"
CREATE INDEX "idx_dead_letter_queues_stripe_event_id" ON "public"."dead_letter_queues" ("stripe_event_id");
-- Create index "idx_dead_letter_queues_task_type" to table: "dead_letter_queues"
CREATE INDEX "idx_dead_letter_queues_task_type" ON "public"."dead_letter_queues" ("task_type");
-- Create "email_outbox" table
CREATE TABLE "public"."email_outbox" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "event_type" character varying(50) NOT NULL,
  "recipient_email" character varying(255) NOT NULL,
  "subject" text NOT NULL,
  "body_html" text NULL,
  "body_text" text NULL,
  "template_data" jsonb NULL,
  "priority" integer NULL DEFAULT 1,
  "status" character varying(20) NULL DEFAULT 'pending',
  "max_retries" integer NULL DEFAULT 3,
  "retry_count" integer NULL DEFAULT 0,
  "last_attempt_at" timestamp NULL,
  "next_attempt_at" timestamp NULL DEFAULT now(),
  "error_message" text NULL,
  "created_at" timestamp NULL DEFAULT now(),
  "updated_at" timestamp NULL DEFAULT now()
);
-- Create "email_outboxes" table
CREATE TABLE "public"."email_outboxes" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "event_type" character varying(50) NOT NULL,
  "recipient_email" character varying(255) NOT NULL,
  "subject" text NOT NULL,
  "body_html" text NULL,
  "body_text" text NULL,
  "template_data" jsonb NULL,
  "priority" integer NULL DEFAULT 1,
  "status" character varying(20) NULL DEFAULT 'pending',
  "max_retries" integer NULL DEFAULT 3,
  "retry_count" integer NULL DEFAULT 0,
  "last_attempt_at" timestamp NULL,
  "next_attempt_at" timestamp NULL DEFAULT now(),
  "error_message" text NULL,
  "created_at" timestamp NULL DEFAULT now(),
  "updated_at" timestamp NULL DEFAULT now(),
  CONSTRAINT "email_outbox_pkey" PRIMARY KEY ("id")
);
-- Create index "idx_email_outbox_event_type" to table: "email_outboxes"
CREATE INDEX "idx_email_outbox_event_type" ON "public"."email_outboxes" ("event_type");
-- Create index "idx_email_outbox_recipient" to table: "email_outboxes"
CREATE INDEX "idx_email_outbox_recipient" ON "public"."email_outboxes" ("recipient_email");
-- Create index "idx_email_outbox_status_next_attempt" to table: "email_outboxes"
CREATE INDEX "idx_email_outbox_status_next_attempt" ON "public"."email_outboxes" ("status", "next_attempt_at");
-- Create "event_sales" table
CREATE TABLE "public"."event_sales" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "event_id" uuid NOT NULL,
  "organizer_id" uuid NOT NULL,
  "total_tickets_sold" bigint NOT NULL DEFAULT 0,
  "gross_revenue" numeric NOT NULL DEFAULT 0,
  "commission_rate" numeric NOT NULL,
  "commission_amount" numeric NOT NULL DEFAULT 0,
  "organizer_share" numeric NOT NULL DEFAULT 0,
  "paid_amount" numeric NOT NULL DEFAULT 0,
  "due_amount" numeric NOT NULL DEFAULT 0,
  "last_payment_date" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_event_sales_event_id" UNIQUE ("event_id")
);
-- Create index "idx_event_sales_deleted_at" to table: "event_sales"
CREATE INDEX "idx_event_sales_deleted_at" ON "public"."event_sales" ("deleted_at");
-- Create index "idx_event_sales_event_id" to table: "event_sales"
CREATE INDEX "idx_event_sales_event_id" ON "public"."event_sales" ("event_id");
-- Create index "idx_event_sales_organizer_id" to table: "event_sales"
CREATE INDEX "idx_event_sales_organizer_id" ON "public"."event_sales" ("organizer_id");
-- Create "event_status_histories" table
CREATE TABLE "public"."event_status_histories" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "event_id" uuid NULL,
  "old_status" character varying(50) NULL,
  "new_status" character varying(50) NULL,
  "status_type" character varying(20) NULL,
  "changed_by" uuid NULL,
  "remark" text NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "chk_event_status_histories_status_type" CHECK ((status_type)::text = ANY (ARRAY[('approval'::character varying)::text, ('sales'::character varying)::text]))
);
-- Create index "idx_event_status_histories_changed_by" to table: "event_status_histories"
CREATE INDEX "idx_event_status_histories_changed_by" ON "public"."event_status_histories" ("changed_by");
-- Create index "idx_event_status_histories_event_id" to table: "event_status_histories"
CREATE INDEX "idx_event_status_histories_event_id" ON "public"."event_status_histories" ("event_id");
-- Create "webhook_events" table
CREATE TABLE "public"."webhook_events" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "provider" text NOT NULL,
  "event_id" text NULL,
  "event_type" text NOT NULL,
  "status" text NOT NULL,
  "payload" jsonb NULL,
  "headers" jsonb NULL,
  "payment_intent_id" uuid NULL,
  "transaction_id" uuid NULL,
  "refund_id" uuid NULL,
  "received_at" timestamptz NOT NULL,
  "processed_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_webhook_events_event_type" to table: "webhook_events"
CREATE INDEX "idx_webhook_events_event_type" ON "public"."webhook_events" ("event_type");
-- Create index "idx_webhook_events_payment_intent_id" to table: "webhook_events"
CREATE INDEX "idx_webhook_events_payment_intent_id" ON "public"."webhook_events" ("payment_intent_id");
-- Create index "idx_webhook_events_provider" to table: "webhook_events"
CREATE INDEX "idx_webhook_events_provider" ON "public"."webhook_events" ("provider");
-- Create index "idx_webhook_events_status" to table: "webhook_events"
CREATE INDEX "idx_webhook_events_status" ON "public"."webhook_events" ("status");
-- Create index "idx_webhook_events_transaction_id" to table: "webhook_events"
CREATE INDEX "idx_webhook_events_transaction_id" ON "public"."webhook_events" ("transaction_id");
-- Create "events" table
CREATE TABLE "public"."events" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "title" character varying(200) NOT NULL,
  "description" text NULL,
  "event_type" character varying(50) NULL DEFAULT 'event',
  "banner_image" character varying(500) NULL,
  "category" text NULL,
  "venue_name" character varying(200) NULL,
  "address" character varying(200) NULL,
  "location" character varying(200) NULL,
  "start_date" timestamptz NOT NULL,
  "end_date" timestamptz NOT NULL,
  "timezone" character varying(50) NULL DEFAULT 'UTC',
  "capacity" bigint NOT NULL,
  "available" bigint NOT NULL,
  "commission_rate" numeric NOT NULL DEFAULT 10,
  "status" text NOT NULL DEFAULT 'draft',
  "sales_status" text NOT NULL DEFAULT 'active',
  "is_featured" boolean NOT NULL DEFAULT false,
  "is_cancelled" boolean NOT NULL DEFAULT false,
  "cancelled_at" timestamptz NULL,
  "cancel_reason" text NULL,
  "organizer_id" uuid NULL,
  "admin_remark" text NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "currency" character varying(3) NULL,
  "payment_provider" character varying(20) NULL DEFAULT 'STRIPE',
  "is_refundable" boolean NOT NULL DEFAULT true,
  "refund_policy" text NULL,
  "country" character varying(100) NULL,
  "price" numeric NOT NULL DEFAULT 0,
  PRIMARY KEY ("id")
);
-- Create index "idx_events_deleted_at" to table: "events"
CREATE INDEX "idx_events_deleted_at" ON "public"."events" ("deleted_at");
-- Create index "idx_events_organizer_id" to table: "events"
CREATE INDEX "idx_events_organizer_id" ON "public"."events" ("organizer_id");
-- Create "file_storages" table
CREATE TABLE "public"."file_storages" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "file_name" character varying(255) NOT NULL,
  "original_name" character varying(255) NOT NULL,
  "file_size" bigint NOT NULL,
  "mime_type" character varying(100) NOT NULL,
  "file_path" character varying(500) NOT NULL,
  "public_url" character varying(500) NOT NULL,
  "bucket_name" character varying(100) NOT NULL,
  "region" character varying(50) NOT NULL,
  "category" character varying(50) NOT NULL DEFAULT 'other',
  "event_id" uuid NULL,
  "organizer_id" uuid NULL,
  "user_id" uuid NULL,
  "company_id" uuid NULL,
  "category_id" uuid NULL,
  "width" bigint NULL,
  "height" bigint NULL,
  "alt_text" character varying(255) NULL,
  "description" text NULL,
  "tags" text[] NULL,
  "is_public" boolean NOT NULL DEFAULT true,
  "is_active" boolean NOT NULL DEFAULT true,
  "uploaded_by" uuid NOT NULL,
  "uploaded_at" timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "expires_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_file_storages_category_id" to table: "file_storages"
CREATE INDEX "idx_file_storages_category_id" ON "public"."file_storages" ("category_id");
-- Create index "idx_file_storages_company_id" to table: "file_storages"
CREATE INDEX "idx_file_storages_company_id" ON "public"."file_storages" ("company_id");
-- Create index "idx_file_storages_deleted_at" to table: "file_storages"
CREATE INDEX "idx_file_storages_deleted_at" ON "public"."file_storages" ("deleted_at");
-- Create index "idx_file_storages_event_id" to table: "file_storages"
CREATE INDEX "idx_file_storages_event_id" ON "public"."file_storages" ("event_id");
-- Create index "idx_file_storages_expires_at" to table: "file_storages"
CREATE INDEX "idx_file_storages_expires_at" ON "public"."file_storages" ("expires_at");
-- Create index "idx_file_storages_organizer_id" to table: "file_storages"
CREATE INDEX "idx_file_storages_organizer_id" ON "public"."file_storages" ("organizer_id");
-- Create index "idx_file_storages_uploaded_by" to table: "file_storages"
CREATE INDEX "idx_file_storages_uploaded_by" ON "public"."file_storages" ("uploaded_by");
-- Create index "idx_file_storages_user_id" to table: "file_storages"
CREATE INDEX "idx_file_storages_user_id" ON "public"."file_storages" ("user_id");
-- Create "user_roles" table
CREATE TABLE "public"."user_roles" (
  "user_id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "role_id" uuid NOT NULL DEFAULT gen_random_uuid(),
  PRIMARY KEY ("user_id", "role_id")
);
-- Create "individual_tickets" table
CREATE TABLE "public"."individual_tickets" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "ticket_id" uuid NOT NULL,
  "ticket_number" character varying(50) NOT NULL,
  "qr_code" character varying(500) NULL,
  "status" text NOT NULL DEFAULT 'active',
  "check_in_time" timestamptz NULL,
  "check_out_time" timestamptz NULL,
  "checked_in_by" uuid NULL,
  "checked_out_by" uuid NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_individual_tickets_ticket_number" UNIQUE ("ticket_number")
);
-- Create index "idx_individual_tickets_deleted_at" to table: "individual_tickets"
CREATE INDEX "idx_individual_tickets_deleted_at" ON "public"."individual_tickets" ("deleted_at");
-- Create index "idx_individual_tickets_ticket_id" to table: "individual_tickets"
CREATE INDEX "idx_individual_tickets_ticket_id" ON "public"."individual_tickets" ("ticket_id");
-- Create index "idx_individual_tickets_ticket_number" to table: "individual_tickets"
CREATE INDEX "idx_individual_tickets_ticket_number" ON "public"."individual_tickets" ("ticket_number");
-- Create "invoices" table
CREATE TABLE "public"."invoices" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "invoice_number" character varying(50) NOT NULL,
  "transaction_id" uuid NOT NULL,
  "customer_name" character varying(255) NOT NULL,
  "customer_email" character varying(255) NOT NULL,
  "customer_phone" character varying(50) NULL,
  "amount" numeric(10,2) NOT NULL,
  "currency" character varying(3) NOT NULL,
  "tax_amount" numeric(10,2) NULL DEFAULT 0,
  "total_amount" numeric(10,2) NOT NULL,
  "file_url" text NULL,
  "file_key" text NULL,
  "receipt_url" text NULL,
  "status" character varying(50) NOT NULL DEFAULT 'generated',
  "sent_at" timestamptz NULL,
  "viewed_at" timestamptz NULL,
  "metadata" jsonb NULL,
  "issued_at" timestamptz NOT NULL,
  "due_date" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_invoices_invoice_number" UNIQUE ("invoice_number"),
  CONSTRAINT "uni_invoices_transaction_id" UNIQUE ("transaction_id")
);
-- Create index "idx_invoices_deleted_at" to table: "invoices"
CREATE INDEX "idx_invoices_deleted_at" ON "public"."invoices" ("deleted_at");
-- Create index "idx_invoices_transaction_id" to table: "invoices"
CREATE INDEX "idx_invoices_transaction_id" ON "public"."invoices" ("transaction_id");
-- Create "ledger_entries" table
CREATE TABLE "public"."ledger_entries" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "organizer_id" uuid NOT NULL,
  "transaction_id" uuid NULL,
  "type" character varying(20) NOT NULL,
  "amount_local" numeric(10,2) NOT NULL,
  "currency" character varying(3) NOT NULL,
  "amount_base" numeric(10,2) NOT NULL,
  "base_currency" character varying(3) NULL DEFAULT 'USD',
  "exchange_rate" numeric(10,6) NOT NULL,
  "description" character varying(255) NULL,
  "metadata" jsonb NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_ledger_entries_organizer_id" to table: "ledger_entries"
CREATE INDEX "idx_ledger_entries_organizer_id" ON "public"."ledger_entries" ("organizer_id");
-- Create index "idx_ledger_entries_transaction_id" to table: "ledger_entries"
CREATE INDEX "idx_ledger_entries_transaction_id" ON "public"."ledger_entries" ("transaction_id");
-- Create index "idx_ledger_entries_type" to table: "ledger_entries"
CREATE INDEX "idx_ledger_entries_type" ON "public"."ledger_entries" ("type");
-- Create "organizations" table
CREATE TABLE "public"."organizations" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" text NOT NULL,
  "description" text NULL,
  "logo_url" text NULL,
  "website_url" text NULL,
  "organizer_id" uuid NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_organizations_deleted_at" to table: "organizations"
CREATE INDEX "idx_organizations_deleted_at" ON "public"."organizations" ("deleted_at");
-- Create "organizer_onboardings" table
CREATE TABLE "public"."organizer_onboardings" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "organizer_id" uuid NULL,
  "is_profile_complete" boolean NOT NULL DEFAULT false,
  "is_business_info_complete" boolean NOT NULL DEFAULT false,
  "is_categories_selected" boolean NOT NULL DEFAULT false,
  "is_onboarding_complete" boolean NOT NULL DEFAULT false,
  "business_name" character varying(200) NULL,
  "business_description" text NULL,
  "business_logo_url" character varying(500) NULL,
  "business_website_url" character varying(500) NULL,
  "business_email" character varying(200) NULL,
  "business_phone" character varying(50) NULL,
  "business_address" text NULL,
  "business_categories" text[] NULL,
  "years_of_experience" bigint NULL DEFAULT 0,
  "specialties" text[] NULL,
  "services_offered" text[] NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "is_complete" boolean NOT NULL DEFAULT false,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_organizer_onboardings_organizer_id" UNIQUE ("organizer_id")
);
-- Create index "idx_organizer_onboardings_deleted_at" to table: "organizer_onboardings"
CREATE INDEX "idx_organizer_onboardings_deleted_at" ON "public"."organizer_onboardings" ("deleted_at");
-- Create index "idx_organizer_onboardings_organizer_id" to table: "organizer_onboardings"
CREATE INDEX "idx_organizer_onboardings_organizer_id" ON "public"."organizer_onboardings" ("organizer_id");
-- Create "organizer_tier_templates" table
CREATE TABLE "public"."organizer_tier_templates" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "organizer_id" uuid NOT NULL,
  "template_name" character varying(100) NOT NULL,
  "description" text NULL,
  "is_active" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_organizer_template_name" to table: "organizer_tier_templates"
CREATE UNIQUE INDEX "idx_organizer_template_name" ON "public"."organizer_tier_templates" ("organizer_id", "template_name") WHERE (deleted_at IS NULL);
-- Create index "idx_organizer_tier_templates_deleted_at" to table: "organizer_tier_templates"
CREATE INDEX "idx_organizer_tier_templates_deleted_at" ON "public"."organizer_tier_templates" ("deleted_at");
-- Create index "idx_organizer_tier_templates_organizer_id" to table: "organizer_tier_templates"
CREATE INDEX "idx_organizer_tier_templates_organizer_id" ON "public"."organizer_tier_templates" ("organizer_id");
-- Create "otps" table
CREATE TABLE "public"."otps" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "identifier" text NOT NULL,
  "type" text NOT NULL,
  "role" text NOT NULL DEFAULT 'user',
  "code" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "attempts" bigint NULL DEFAULT 0,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_otp_identifier_type_role" to table: "otps"
CREATE INDEX "idx_otp_identifier_type_role" ON "public"."otps" ("identifier", "type", "role");
-- Create index "idx_otps_expires_at" to table: "otps"
CREATE INDEX "idx_otps_expires_at" ON "public"."otps" ("expires_at");
-- Create "checkout_sessions" table
CREATE TABLE "public"."checkout_sessions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "ticket_id" uuid NOT NULL,
  "guest_user_id" uuid NULL,
  "checkout_token" text NOT NULL,
  "payment_gateway" text NOT NULL,
  "amount" numeric NOT NULL,
  "currency" text NULL DEFAULT 'NPR',
  "status" text NULL DEFAULT 'PENDING',
  "gateway_data" jsonb NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "user_id" uuid NULL,
  "stripe_session_id" text NULL,
  "payment_intent_id" uuid NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_checkout_sessions_checkout_token" to table: "checkout_sessions"
CREATE UNIQUE INDEX "idx_checkout_sessions_checkout_token" ON "public"."checkout_sessions" ("checkout_token");
-- Create index "idx_checkout_sessions_payment_intent_id" to table: "checkout_sessions"
CREATE INDEX "idx_checkout_sessions_payment_intent_id" ON "public"."checkout_sessions" ("payment_intent_id");
-- Create index "idx_checkout_sessions_stripe_session_id" to table: "checkout_sessions"
CREATE INDEX "idx_checkout_sessions_stripe_session_id" ON "public"."checkout_sessions" ("stripe_session_id");
-- Create "categories" table
CREATE TABLE "public"."categories" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" character varying(100) NOT NULL,
  "description" text NULL,
  "icon_url" character varying(500) NULL,
  "is_active" boolean NOT NULL DEFAULT true,
  "sort_order" bigint NOT NULL DEFAULT 0,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_categories_name" UNIQUE ("name")
);
-- Create index "idx_categories_deleted_at" to table: "categories"
CREATE INDEX "idx_categories_deleted_at" ON "public"."categories" ("deleted_at");
-- Create "company_infos" table
CREATE TABLE "public"."company_infos" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" character varying(200) NOT NULL,
  "description" text NULL,
  "logo_url" character varying(500) NULL,
  "email" character varying(200) NULL,
  "phone" character varying(50) NULL,
  "address" text NULL,
  "website_url" character varying(500) NULL,
  "facebook_url" character varying(500) NULL,
  "twitter_url" character varying(500) NULL,
  "instagram_url" character varying(500) NULL,
  "linked_in_url" character varying(500) NULL,
  "you_tube_url" character varying(500) NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_company_infos_deleted_at" to table: "company_infos"
CREATE INDEX "idx_company_infos_deleted_at" ON "public"."company_infos" ("deleted_at");
-- Create "payment_gateway_configs" table
CREATE TABLE "public"."payment_gateway_configs" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "gateway_name" character varying(50) NOT NULL,
  "display_name" character varying(100) NOT NULL,
  "is_enabled" boolean NULL DEFAULT false,
  "is_test_mode" boolean NULL DEFAULT true,
  "priority" bigint NULL DEFAULT 0,
  "supported_countries" text[] NULL,
  "supported_currencies" text[] NULL,
  "api_key" text NULL,
  "api_secret" text NULL,
  "webhook_secret" text NULL,
  "config" jsonb NULL,
  "percentage_fee" numeric(5,2) NULL DEFAULT 0,
  "fixed_fee" numeric(10,2) NULL DEFAULT 0,
  "min_amount" numeric(10,2) NULL,
  "max_amount" numeric(10,2) NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "api_key_encrypted" text NULL,
  "api_secret_encrypted" text NULL,
  "webhook_secret_encrypted" text NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_payment_gateway_configs_gateway_name" UNIQUE ("gateway_name")
);
-- Create index "idx_payment_gateway_configs_deleted_at" to table: "payment_gateway_configs"
CREATE INDEX "idx_payment_gateway_configs_deleted_at" ON "public"."payment_gateway_configs" ("deleted_at");
-- Create index "idx_payment_gateway_configs_gateway_name" to table: "payment_gateway_configs"
CREATE INDEX "idx_payment_gateway_configs_gateway_name" ON "public"."payment_gateway_configs" ("gateway_name");
-- Create "payment_histories" table
CREATE TABLE "public"."payment_histories" (
  "id" bigserial NOT NULL,
  "payment_bill_id" uuid NOT NULL,
  "amount" numeric NOT NULL,
  "payment_method" text NOT NULL,
  "payment_ref" text NULL,
  "payment_date" timestamptz NULL,
  "processed_by_id" uuid NOT NULL,
  "notes" text NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "screenshot_url" character varying(500) NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_payment_histories_payment_bill_id" to table: "payment_histories"
CREATE INDEX "idx_payment_histories_payment_bill_id" ON "public"."payment_histories" ("payment_bill_id");
-- Create "payment_intents" table
CREATE TABLE "public"."payment_intents" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "actor_id" uuid NOT NULL,
  "actor_type" text NOT NULL,
  "event_id" uuid NOT NULL,
  "tier_id" uuid NOT NULL,
  "customer_email" text NOT NULL,
  "quantity" bigint NOT NULL,
  "amount_total" bigint NOT NULL,
  "currency" text NOT NULL,
  "status" text NULL,
  "session_id" text NULL,
  "checkout_token" text NULL,
  "idempotency_key" text NULL,
  "payment_gateway" text NULL,
  "timezone" text NULL,
  "gateway_metadata" jsonb NULL,
  "expires_at" timestamptz NULL,
  "succeeded_at" timestamptz NULL,
  "canceled_at" timestamptz NULL,
  "failed_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_payment_intents_actor_id" to table: "payment_intents"
CREATE INDEX "idx_payment_intents_actor_id" ON "public"."payment_intents" ("actor_id");
-- Create index "idx_payment_intents_checkout_token" to table: "payment_intents"
CREATE INDEX "idx_payment_intents_checkout_token" ON "public"."payment_intents" ("checkout_token");
-- Create index "idx_payment_intents_event_id" to table: "payment_intents"
CREATE INDEX "idx_payment_intents_event_id" ON "public"."payment_intents" ("event_id");
-- Create index "idx_payment_intents_session_id" to table: "payment_intents"
CREATE INDEX "idx_payment_intents_session_id" ON "public"."payment_intents" ("session_id");
-- Create index "idx_payment_intents_tier_id" to table: "payment_intents"
CREATE INDEX "idx_payment_intents_tier_id" ON "public"."payment_intents" ("tier_id");
-- Create "payout_requests" table
CREATE TABLE "public"."payout_requests" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "request_number" character varying(50) NOT NULL,
  "organizer_id" uuid NOT NULL,
  "event_id" uuid NULL,
  "amount" numeric NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "request_type" text NOT NULL DEFAULT 'event_payout',
  "description" text NULL,
  "admin_notes" text NULL,
  "processed_by" uuid NULL,
  "processed_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "payment_bill_id" uuid NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_payout_requests_request_number" UNIQUE ("request_number")
);
-- Create index "idx_payout_requests_deleted_at" to table: "payout_requests"
CREATE INDEX "idx_payout_requests_deleted_at" ON "public"."payout_requests" ("deleted_at");
-- Create index "idx_payout_requests_event_id" to table: "payout_requests"
CREATE INDEX "idx_payout_requests_event_id" ON "public"."payout_requests" ("event_id");
-- Create index "idx_payout_requests_organizer_id" to table: "payout_requests"
CREATE INDEX "idx_payout_requests_organizer_id" ON "public"."payout_requests" ("organizer_id");
-- Create index "idx_payout_requests_payment_bill_id" to table: "payout_requests"
CREATE INDEX "idx_payout_requests_payment_bill_id" ON "public"."payout_requests" ("payment_bill_id");
-- Create "permissions" table
CREATE TABLE "public"."permissions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" text NOT NULL,
  "description" text NULL,
  "resource" text NOT NULL,
  "action" text NOT NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_permissions_name" UNIQUE ("name")
);
-- Create "processing_locks" table
CREATE TABLE "public"."processing_locks" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "lock_key" character varying(255) NOT NULL,
  "lock_type" character varying(50) NOT NULL,
  "owner_id" character varying(255) NOT NULL,
  "expires_at" timestamp NOT NULL,
  "created_at" timestamp NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "processing_locks_lock_key_key" UNIQUE ("lock_key")
);
-- Create index "idx_processing_locks_expires" to table: "processing_locks"
CREATE INDEX "idx_processing_locks_expires" ON "public"."processing_locks" ("expires_at");
-- Create index "idx_processing_locks_key" to table: "processing_locks"
CREATE INDEX "idx_processing_locks_key" ON "public"."processing_locks" ("lock_key");
-- Create index "idx_processing_locks_type_expires" to table: "processing_locks"
CREATE INDEX "idx_processing_locks_type_expires" ON "public"."processing_locks" ("lock_type", "expires_at");
-- Create "refund_status_history" table
CREATE TABLE "public"."refund_status_history" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "refund_id" uuid NOT NULL,
  "old_status" character varying(50) NULL,
  "new_status" character varying(50) NOT NULL,
  "changed_by_id" uuid NULL,
  "changed_by_type" character varying(50) NULL DEFAULT 'system',
  "remarks" text NULL,
  "metadata" jsonb NULL,
  "changed_at" timestamptz NOT NULL,
  "created_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_refund_status_history_changed_at" to table: "refund_status_history"
CREATE INDEX "idx_refund_status_history_changed_at" ON "public"."refund_status_history" ("changed_at");
-- Create index "idx_refund_status_history_changed_by_id" to table: "refund_status_history"
CREATE INDEX "idx_refund_status_history_changed_by_id" ON "public"."refund_status_history" ("changed_by_id");
-- Create index "idx_refund_status_history_refund_id" to table: "refund_status_history"
CREATE INDEX "idx_refund_status_history_refund_id" ON "public"."refund_status_history" ("refund_id");
-- Create "refunds" table
CREATE TABLE "public"."refunds" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "refund_number" text NULL,
  "transaction_id" uuid NOT NULL,
  "payment_intent_id" uuid NOT NULL,
  "event_id" uuid NOT NULL,
  "actor_id" uuid NOT NULL,
  "actor_type" text NOT NULL,
  "approved_by_id" uuid NULL,
  "approved_by_type" text NULL,
  "provider" text NOT NULL,
  "provider_refund_id" text NULL,
  "amount" bigint NOT NULL,
  "currency" text NOT NULL,
  "reason" text NULL,
  "affected_ticket_ids" jsonb NULL,
  "status" text NOT NULL,
  "is_full_refund" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_refunds_actor_id" to table: "refunds"
CREATE INDEX "idx_refunds_actor_id" ON "public"."refunds" ("actor_id");
-- Create index "idx_refunds_event_id" to table: "refunds"
CREATE INDEX "idx_refunds_event_id" ON "public"."refunds" ("event_id");
-- Create index "idx_refunds_payment_intent_id" to table: "refunds"
CREATE INDEX "idx_refunds_payment_intent_id" ON "public"."refunds" ("payment_intent_id");
-- Create index "idx_refunds_provider" to table: "refunds"
CREATE INDEX "idx_refunds_provider" ON "public"."refunds" ("provider");
-- Create index "idx_refunds_provider_refund_id" to table: "refunds"
CREATE INDEX "idx_refunds_provider_refund_id" ON "public"."refunds" ("provider_refund_id");
-- Create index "idx_refunds_status" to table: "refunds"
CREATE INDEX "idx_refunds_status" ON "public"."refunds" ("status");
-- Create index "idx_refunds_transaction_id" to table: "refunds"
CREATE INDEX "idx_refunds_transaction_id" ON "public"."refunds" ("transaction_id");
-- Create "registration_requests" table
CREATE TABLE "public"."registration_requests" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "email" text NOT NULL,
  "first_name" text NOT NULL,
  "last_name" text NOT NULL,
  "phone" text NOT NULL,
  "country_code" text NOT NULL,
  "password" text NOT NULL,
  "user_type" text NOT NULL,
  "is_verified" boolean NULL DEFAULT false,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_registration_requests_email" to table: "registration_requests"
CREATE UNIQUE INDEX "idx_registration_requests_email" ON "public"."registration_requests" ("email");
-- Create "role_permissions" table
CREATE TABLE "public"."role_permissions" (
  "permission_id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "role_id" uuid NOT NULL DEFAULT gen_random_uuid(),
  PRIMARY KEY ("permission_id", "role_id")
);
-- Create "roles" table
CREATE TABLE "public"."roles" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" text NOT NULL,
  "description" text NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_roles_name" UNIQUE ("name")
);
-- Create "schema_migrations" table
CREATE TABLE "public"."schema_migrations" (
  "version" bigint NOT NULL,
  "dirty" boolean NOT NULL,
  PRIMARY KEY ("version")
);
-- Create "transactions" table
CREATE TABLE "public"."transactions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "payment_intent_id" uuid NOT NULL,
  "payment_attempt_id" uuid NOT NULL,
  "event_id" uuid NOT NULL,
  "actor_id" uuid NOT NULL,
  "actor_type" text NOT NULL,
  "provider" text NOT NULL,
  "provider_txn_id" text NULL,
  "amount_total" bigint NOT NULL,
  "currency" text NOT NULL,
  "platform_fee" bigint NOT NULL,
  "gateway_fee" bigint NOT NULL,
  "organizer_earning" bigint NOT NULL,
  "quantity" bigint NOT NULL,
  "status" text NOT NULL,
  "is_paid_out" boolean NOT NULL DEFAULT false,
  "paid_out_at" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_transactions_actor_id" to table: "transactions"
CREATE INDEX "idx_transactions_actor_id" ON "public"."transactions" ("actor_id");
-- Create index "idx_transactions_event_id" to table: "transactions"
CREATE INDEX "idx_transactions_event_id" ON "public"."transactions" ("event_id");
-- Create index "idx_transactions_payment_attempt_id" to table: "transactions"
CREATE INDEX "idx_transactions_payment_attempt_id" ON "public"."transactions" ("payment_attempt_id");
-- Create index "idx_transactions_payment_intent_id" to table: "transactions"
CREATE INDEX "idx_transactions_payment_intent_id" ON "public"."transactions" ("payment_intent_id");
-- Create index "idx_transactions_provider" to table: "transactions"
CREATE INDEX "idx_transactions_provider" ON "public"."transactions" ("provider");
-- Create index "idx_transactions_provider_txn_id" to table: "transactions"
CREATE INDEX "idx_transactions_provider_txn_id" ON "public"."transactions" ("provider_txn_id");
-- Create index "idx_transactions_status" to table: "transactions"
CREATE INDEX "idx_transactions_status" ON "public"."transactions" ("status");
-- Create "tickets" table
CREATE TABLE "public"."tickets" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "ticket_number" character varying(50) NOT NULL,
  "user_id" uuid NULL,
  "event_id" uuid NOT NULL,
  "quantity" bigint NOT NULL DEFAULT 1,
  "total_amount" numeric NOT NULL,
  "status" text NOT NULL DEFAULT 'ACTIVE',
  "check_in_time" timestamptz NULL,
  "check_out_time" timestamptz NULL,
  "checked_in_by" uuid NULL,
  "checked_out_by" uuid NULL,
  "purchase_date" timestamptz NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "guest_user_id" uuid NULL,
  "is_guest_purchase" boolean NULL DEFAULT false,
  "checked_in_count" bigint NULL DEFAULT 0,
  "tier_id" uuid NOT NULL,
  "payment_gateway" text NOT NULL,
  "transaction_id" uuid NULL,
  "payment_status" character varying(20) NULL DEFAULT 'PENDING',
  "paid_at" timestamp NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_tickets_ticket_number" UNIQUE ("ticket_number")
);
-- Create index "idx_tickets_deleted_at" to table: "tickets"
CREATE INDEX "idx_tickets_deleted_at" ON "public"."tickets" ("deleted_at");
-- Create index "idx_tickets_event_id" to table: "tickets"
CREATE INDEX "idx_tickets_event_id" ON "public"."tickets" ("event_id");
-- Create index "idx_tickets_guest_user_id" to table: "tickets"
CREATE INDEX "idx_tickets_guest_user_id" ON "public"."tickets" ("guest_user_id");
-- Create index "idx_tickets_paid_at" to table: "tickets"
CREATE INDEX "idx_tickets_paid_at" ON "public"."tickets" ("paid_at") WHERE (paid_at IS NOT NULL);
-- Create index "idx_tickets_payment_status" to table: "tickets"
CREATE INDEX "idx_tickets_payment_status" ON "public"."tickets" ("payment_status");
-- Create index "idx_tickets_tier_id" to table: "tickets"
CREATE INDEX "idx_tickets_tier_id" ON "public"."tickets" ("tier_id");
-- Create index "idx_tickets_transaction_id" to table: "tickets"
CREATE INDEX "idx_tickets_transaction_id" ON "public"."tickets" ("transaction_id");
-- Create index "idx_tickets_user_id" to table: "tickets"
CREATE INDEX "idx_tickets_user_id" ON "public"."tickets" ("user_id");
-- Create "tokens" table
CREATE TABLE "public"."tokens" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NULL,
  "token_hash" text NOT NULL,
  "type" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "revoked" boolean NULL DEFAULT false,
  "device" text NULL,
  "ip" text NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_tokens_user_id" to table: "tokens"
CREATE INDEX "idx_tokens_user_id" ON "public"."tokens" ("user_id");
-- Create "guest_users" table
CREATE TABLE "public"."guest_users" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "email" character varying(255) NOT NULL,
  "first_name" character varying(100) NULL,
  "last_name" character varying(100) NULL,
  "phone" character varying(20) NULL,
  "country_code" character varying(5) NULL,
  "email_verified" boolean NULL DEFAULT false,
  "verification_token" character varying(255) NULL,
  "token_expires_at" timestamptz NULL,
  "converted_to_user" boolean NULL DEFAULT false,
  "converted_user_id" uuid NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_guest_users_converted_user_id" to table: "guest_users"
CREATE INDEX "idx_guest_users_converted_user_id" ON "public"."guest_users" ("converted_user_id");
-- Create index "idx_guest_users_deleted_at" to table: "guest_users"
CREATE INDEX "idx_guest_users_deleted_at" ON "public"."guest_users" ("deleted_at");
-- Create index "idx_guest_users_email" to table: "guest_users"
CREATE INDEX "idx_guest_users_email" ON "public"."guest_users" ("email");
-- Create index "idx_guest_users_verification_token" to table: "guest_users"
CREATE INDEX "idx_guest_users_verification_token" ON "public"."guest_users" ("verification_token");
-- Create "event_tiers" table
CREATE TABLE "public"."event_tiers" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "event_id" uuid NOT NULL,
  "tier_template_id" uuid NOT NULL,
  "tier_name" character varying(100) NOT NULL,
  "price" numeric NOT NULL,
  "quantity" bigint NOT NULL,
  "available" bigint NOT NULL,
  "sold" bigint NOT NULL DEFAULT 0,
  "gst" numeric NULL DEFAULT 0,
  "sales_start" timestamptz NULL,
  "sales_end" timestamptz NULL,
  "is_active" boolean NOT NULL DEFAULT true,
  "sort_order" bigint NULL DEFAULT 0,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "currency" character varying(3) NOT NULL DEFAULT 'USD',
  "reserved" bigint NOT NULL DEFAULT 0,
  PRIMARY KEY ("id")
);
-- Create index "idx_event_tiers_deleted_at" to table: "event_tiers"
CREATE INDEX "idx_event_tiers_deleted_at" ON "public"."event_tiers" ("deleted_at");
-- Create index "idx_event_tiers_event_id" to table: "event_tiers"
CREATE INDEX "idx_event_tiers_event_id" ON "public"."event_tiers" ("event_id");
-- Create index "idx_event_tiers_tier_template_id" to table: "event_tiers"
CREATE INDEX "idx_event_tiers_tier_template_id" ON "public"."event_tiers" ("tier_template_id");
-- Create "users" table
CREATE TABLE "public"."users" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "email" text NOT NULL,
  "password_hash" text NOT NULL,
  "first_name" text NULL,
  "last_name" text NULL,
  "phone" text NULL,
  "is_email_verified" boolean NULL DEFAULT false,
  "verification_code" text NULL,
  "organization_id" uuid NULL,
  "created_by" uuid NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "country_code" text NULL,
  "organizer_status" text NULL DEFAULT 'inactive',
  "account_status" text NULL DEFAULT 'active',
  "admin_remark" text NULL,
  "approved_at" timestamptz NULL,
  "rejected_at" timestamptz NULL,
  "organizer_id" uuid NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uni_users_email" UNIQUE ("email")
);
-- Create index "idx_users_deleted_at" to table: "users"
CREATE INDEX "idx_users_deleted_at" ON "public"."users" ("deleted_at");
-- Create index "idx_users_organization_id" to table: "users"
CREATE INDEX "idx_users_organization_id" ON "public"."users" ("organization_id");
-- Create index "idx_users_organizer_id" to table: "users"
CREATE INDEX "idx_users_organizer_id" ON "public"."users" ("organizer_id");
-- Create "ticket_reservations" table
CREATE TABLE "public"."ticket_reservations" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "checkout_token" character varying(255) NOT NULL,
  "event_id" uuid NOT NULL,
  "tier_id" uuid NOT NULL,
  "user_id" uuid NULL,
  "guest_user_id" uuid NULL,
  "customer_email" character varying(255) NOT NULL,
  "quantity" integer NOT NULL,
  "status" character varying(20) NULL DEFAULT 'reserved',
  "expires_at" timestamp NOT NULL,
  "confirmed_at" timestamp NULL,
  "created_at" timestamp NULL DEFAULT now(),
  "updated_at" timestamp NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_ticket_reservations_event" FOREIGN KEY ("event_id") REFERENCES "public"."events" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "fk_ticket_reservations_guest" FOREIGN KEY ("guest_user_id") REFERENCES "public"."guest_users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "fk_ticket_reservations_tier" FOREIGN KEY ("tier_id") REFERENCES "public"."event_tiers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "fk_ticket_reservations_user" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "idx_ticket_reservations_checkout_token" to table: "ticket_reservations"
CREATE INDEX "idx_ticket_reservations_checkout_token" ON "public"."ticket_reservations" ("checkout_token");
-- Create index "idx_ticket_reservations_event_tier" to table: "ticket_reservations"
CREATE INDEX "idx_ticket_reservations_event_tier" ON "public"."ticket_reservations" ("event_id", "tier_id");
-- Create index "idx_ticket_reservations_expires_at" to table: "ticket_reservations"
CREATE INDEX "idx_ticket_reservations_expires_at" ON "public"."ticket_reservations" ("expires_at");
-- Create index "idx_ticket_reservations_status_expires" to table: "ticket_reservations"
CREATE INDEX "idx_ticket_reservations_status_expires" ON "public"."ticket_reservations" ("status", "expires_at");
