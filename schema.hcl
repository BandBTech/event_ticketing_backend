
table "categories" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "name" {
    null = false
    type = character_varying(100)
  }
  column "description" {
    null = true
    type = text
  }
  column "icon_url" {
    null = true
    type = character_varying(500)
  }
  column "is_active" {
    null    = false
    type    = boolean
    default = true
  }
  column "sort_order" {
    null    = false
    type    = bigint
    default = 0
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_categories_deleted_at" {
    columns = [column.deleted_at]
  }
  unique "uni_categories_name" {
    columns = [column.name]
  }
}
table "checkout_sessions" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "ticket_id" {
    null = false
    type = uuid
  }
  column "guest_user_id" {
    null = true
    type = uuid
  }
  column "checkout_token" {
    null = false
    type = text
  }
  column "payment_gateway" {
    null = false
    type = text
  }
  column "amount" {
    null = false
    type = numeric
  }
  column "currency" {
    null    = true
    type    = text
    default = "NPR"
  }
  column "status" {
    null    = true
    type    = text
    default = "PENDING"
  }
  column "gateway_data" {
    null = true
    type = jsonb
  }
  column "expires_at" {
    null = false
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "user_id" {
    null = true
    type = uuid
  }
  column "stripe_session_id" {
    null = true
    type = text
  }
  column "payment_intent_id" {
    null = true
    type = uuid
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_checkout_sessions_checkout_token" {
    unique  = true
    columns = [column.checkout_token]
  }
  index "idx_checkout_sessions_payment_intent_id" {
    columns = [column.payment_intent_id]
  }
  index "idx_checkout_sessions_stripe_session_id" {
    columns = [column.stripe_session_id]
  }
}
table "company_infos" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "name" {
    null = false
    type = character_varying(200)
  }
  column "description" {
    null = true
    type = text
  }
  column "logo_url" {
    null = true
    type = character_varying(500)
  }
  column "email" {
    null = true
    type = character_varying(200)
  }
  column "phone" {
    null = true
    type = character_varying(50)
  }
  column "address" {
    null = true
    type = text
  }
  column "website_url" {
    null = true
    type = character_varying(500)
  }
  column "facebook_url" {
    null = true
    type = character_varying(500)
  }
  column "twitter_url" {
    null = true
    type = character_varying(500)
  }
  column "instagram_url" {
    null = true
    type = character_varying(500)
  }
  column "linked_in_url" {
    null = true
    type = character_varying(500)
  }
  column "you_tube_url" {
    null = true
    type = character_varying(500)
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_company_infos_deleted_at" {
    columns = [column.deleted_at]
  }
}
table "currency_rates" {
  schema = schema.public
  column "id" {
    null = false
    type = serial
  }
  column "base_currency" {
    null = false
    type = character_varying(3)
  }
  column "target_currency" {
    null = false
    type = character_varying(3)
  }
  column "rate" {
    null = false
    type = numeric(20,10)
  }
  column "source" {
    null    = false
    type    = character_varying(50)
    default = "exchangerate.host"
  }
  column "valid_from" {
    null    = false
    type    = timestamptz
    default = sql("CURRENT_TIMESTAMP")
  }
  column "valid_until" {
    null = true
    type = timestamptz
  }
  column "is_active" {
    null    = false
    type    = boolean
    default = true
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_currency_rates_active" {
    on {
      column = column.is_active
    }
    on {
      desc   = true
      column = column.valid_from
    }
  }
  index "idx_currency_rates_base_target" {
    columns = [column.base_currency, column.target_currency]
  }
  index "idx_currency_rates_valid_from" {
    on {
      desc   = true
      column = column.valid_from
    }
  }
  index "idx_currency_rates_valid_until" {
    columns = [column.valid_until]
  }
  unique "currency_rates_base_currency_target_currency_valid_from_key" {
    columns = [column.base_currency, column.target_currency, column.valid_from]
  }
}
table "dead_letter_queues" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "task_type" {
    null = false
    type = character_varying(100)
  }
  column "payload" {
    null = true
    type = jsonb
  }
  column "error" {
    null = true
    type = text
  }
  column "retries" {
    null    = true
    type    = bigint
    default = 0
  }
  column "max_retry" {
    null    = true
    type    = bigint
    default = 5
  }
  column "status" {
    null    = false
    type    = character_varying(20)
    default = "pending"
  }
  column "failed_at" {
    null = false
    type = timestamptz
  }
  column "resolved_at" {
    null = true
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "stripe_event_id" {
    null = true
    type = character_varying(255)
  }
  column "request_id" {
    null = true
    type = character_varying(255)
  }
  column "metadata" {
    null = true
    type = jsonb
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_dead_letter_queues_request_id" {
    columns = [column.request_id]
  }
  index "idx_dead_letter_queues_status" {
    columns = [column.status]
  }
  index "idx_dead_letter_queues_stripe_event_id" {
    columns = [column.stripe_event_id]
  }
  index "idx_dead_letter_queues_task_type" {
    columns = [column.task_type]
  }
}
table "email_outbox" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "event_type" {
    null = false
    type = character_varying(50)
  }
  column "recipient_email" {
    null = false
    type = character_varying(255)
  }
  column "subject" {
    null = false
    type = text
  }
  column "body_html" {
    null = true
    type = text
  }
  column "body_text" {
    null = true
    type = text
  }
  column "template_data" {
    null = true
    type = jsonb
  }
  column "priority" {
    null    = true
    type    = integer
    default = 1
  }
  column "status" {
    null    = true
    type    = character_varying(20)
    default = "pending"
  }
  column "max_retries" {
    null    = true
    type    = integer
    default = 3
  }
  column "retry_count" {
    null    = true
    type    = integer
    default = 0
  }
  column "last_attempt_at" {
    null = true
    type = timestamp
  }
  column "next_attempt_at" {
    null    = true
    type    = timestamp
    default = sql("now()")
  }
  column "error_message" {
    null = true
    type = text
  }
  column "created_at" {
    null    = true
    type    = timestamp
    default = sql("now()")
  }
  column "updated_at" {
    null    = true
    type    = timestamp
    default = sql("now()")
  }
}
table "email_outboxes" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "event_type" {
    null = false
    type = character_varying(50)
  }
  column "recipient_email" {
    null = false
    type = character_varying(255)
  }
  column "subject" {
    null = false
    type = text
  }
  column "body_html" {
    null = true
    type = text
  }
  column "body_text" {
    null = true
    type = text
  }
  column "template_data" {
    null = true
    type = jsonb
  }
  column "priority" {
    null    = true
    type    = integer
    default = 1
  }
  column "status" {
    null    = true
    type    = character_varying(20)
    default = "pending"
  }
  column "max_retries" {
    null    = true
    type    = integer
    default = 3
  }
  column "retry_count" {
    null    = true
    type    = integer
    default = 0
  }
  column "last_attempt_at" {
    null = true
    type = timestamp
  }
  column "next_attempt_at" {
    null    = true
    type    = timestamp
    default = sql("now()")
  }
  column "error_message" {
    null = true
    type = text
  }
  column "created_at" {
    null    = true
    type    = timestamp
    default = sql("now()")
  }
  column "updated_at" {
    null    = true
    type    = timestamp
    default = sql("now()")
  }
  primary_key "email_outbox_pkey" {
    columns = [column.id]
  }
  index "idx_email_outbox_event_type" {
    columns = [column.event_type]
  }
  index "idx_email_outbox_recipient" {
    columns = [column.recipient_email]
  }
  index "idx_email_outbox_status_next_attempt" {
    columns = [column.status, column.next_attempt_at]
  }
}
table "event_sales" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "event_id" {
    null = false
    type = uuid
  }
  column "organizer_id" {
    null = false
    type = uuid
  }
  column "total_tickets_sold" {
    null    = false
    type    = bigint
    default = 0
  }
  column "gross_revenue" {
    null    = false
    type    = numeric
    default = 0
  }
  column "commission_rate" {
    null = false
    type = numeric
  }
  column "commission_amount" {
    null    = false
    type    = numeric
    default = 0
  }
  column "organizer_share" {
    null    = false
    type    = numeric
    default = 0
  }
  column "paid_amount" {
    null    = false
    type    = numeric
    default = 0
  }
  column "due_amount" {
    null    = false
    type    = numeric
    default = 0
  }
  column "last_payment_date" {
    null = true
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_event_sales_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_event_sales_event_id" {
    columns = [column.event_id]
  }
  index "idx_event_sales_organizer_id" {
    columns = [column.organizer_id]
  }
  unique "uni_event_sales_event_id" {
    columns = [column.event_id]
  }
}
table "event_status_histories" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "event_id" {
    null = true
    type = uuid
  }
  column "old_status" {
    null = true
    type = character_varying(50)
  }
  column "new_status" {
    null = true
    type = character_varying(50)
  }
  column "status_type" {
    null = true
    type = character_varying(20)
  }
  column "changed_by" {
    null = true
    type = uuid
  }
  column "remark" {
    null = true
    type = text
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_event_status_histories_changed_by" {
    columns = [column.changed_by]
  }
  index "idx_event_status_histories_event_id" {
    columns = [column.event_id]
  }
  check "chk_event_status_histories_status_type" {
    expr = "((status_type)::text = ANY (ARRAY[('approval'::character varying)::text, ('sales'::character varying)::text]))"
  }
}
table "event_tiers" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "event_id" {
    null = false
    type = uuid
  }
  column "tier_template_id" {
    null = false
    type = uuid
  }
  column "tier_name" {
    null = false
    type = character_varying(100)
  }
  column "price" {
    null = false
    type = numeric
  }
  column "quantity" {
    null = false
    type = bigint
  }
  column "available" {
    null = false
    type = bigint
  }
  column "sold" {
    null    = false
    type    = bigint
    default = 0
  }
  column "gst" {
    null    = true
    type    = numeric
    default = 0
  }
  column "sales_start" {
    null = true
    type = timestamptz
  }
  column "sales_end" {
    null = true
    type = timestamptz
  }
  column "is_active" {
    null    = false
    type    = boolean
    default = true
  }
  column "sort_order" {
    null    = true
    type    = bigint
    default = 0
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  column "currency" {
    null    = false
    type    = character_varying(3)
    default = "USD"
  }
  column "reserved" {
    null    = false
    type    = bigint
    default = 0
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_event_tiers_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_event_tiers_event_id" {
    columns = [column.event_id]
  }
  index "idx_event_tiers_tier_template_id" {
    columns = [column.tier_template_id]
  }
}
table "events" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "title" {
    null = false
    type = character_varying(200)
  }
  column "description" {
    null = true
    type = text
  }
  column "event_type" {
    null    = true
    type    = character_varying(50)
    default = "event"
  }
  column "banner_image" {
    null = true
    type = character_varying(500)
  }
  column "category" {
    null = true
    type = text
  }
  column "venue_name" {
    null = true
    type = character_varying(200)
  }
  column "address" {
    null = true
    type = character_varying(200)
  }
  column "location" {
    null = true
    type = character_varying(200)
  }
  column "start_date" {
    null = false
    type = timestamptz
  }
  column "end_date" {
    null = false
    type = timestamptz
  }
  column "timezone" {
    null    = true
    type    = character_varying(50)
    default = "UTC"
  }
  column "capacity" {
    null = false
    type = bigint
  }
  column "available" {
    null = false
    type = bigint
  }
  column "commission_rate" {
    null    = false
    type    = numeric
    default = 10
  }
  column "status" {
    null    = false
    type    = text
    default = "draft"
  }
  column "sales_status" {
    null    = false
    type    = text
    default = "active"
  }
  column "is_featured" {
    null    = false
    type    = boolean
    default = false
  }
  column "is_cancelled" {
    null    = false
    type    = boolean
    default = false
  }
  column "cancelled_at" {
    null = true
    type = timestamptz
  }
  column "cancel_reason" {
    null = true
    type = text
  }
  column "organizer_id" {
    null = true
    type = uuid
  }
  column "admin_remark" {
    null = true
    type = text
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  column "currency" {
    null    = true
    type    = character_varying(3)
    default = "USD"
  }
  column "payment_provider" {
    null    = true
    type    = character_varying(20)
    default = "STRIPE"
  }
  column "is_refundable" {
    null    = false
    type    = boolean
    default = true
  }
  column "refund_policy" {
    null = true
    type = text
  }
  column "country" {
    null = true
    type = character_varying(100)
  }
  column "price" {
    null    = false
    type    = numeric
    default = 0
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_events_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_events_organizer_id" {
    columns = [column.organizer_id]
  }
}
table "file_storages" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "file_name" {
    null = false
    type = character_varying(255)
  }
  column "original_name" {
    null = false
    type = character_varying(255)
  }
  column "file_size" {
    null = false
    type = bigint
  }
  column "mime_type" {
    null = false
    type = character_varying(100)
  }
  column "file_path" {
    null = false
    type = character_varying(500)
  }
  column "public_url" {
    null = false
    type = character_varying(500)
  }
  column "bucket_name" {
    null = false
    type = character_varying(100)
  }
  column "region" {
    null = false
    type = character_varying(50)
  }
  column "category" {
    null    = false
    type    = character_varying(50)
    default = "other"
  }
  column "event_id" {
    null = true
    type = uuid
  }
  column "organizer_id" {
    null = true
    type = uuid
  }
  column "user_id" {
    null = true
    type = uuid
  }
  column "company_id" {
    null = true
    type = uuid
  }
  column "category_id" {
    null = true
    type = uuid
  }
  column "width" {
    null = true
    type = bigint
  }
  column "height" {
    null = true
    type = bigint
  }
  column "alt_text" {
    null = true
    type = character_varying(255)
  }
  column "description" {
    null = true
    type = text
  }
  column "tags" {
    null = true
    type = sql("text[]")
  }
  column "is_public" {
    null    = false
    type    = boolean
    default = true
  }
  column "is_active" {
    null    = false
    type    = boolean
    default = true
  }
  column "uploaded_by" {
    null = false
    type = uuid
  }
  column "uploaded_at" {
    null    = false
    type    = timestamptz
    default = sql("CURRENT_TIMESTAMP")
  }
  column "expires_at" {
    null = true
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_file_storages_category_id" {
    columns = [column.category_id]
  }
  index "idx_file_storages_company_id" {
    columns = [column.company_id]
  }
  index "idx_file_storages_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_file_storages_event_id" {
    columns = [column.event_id]
  }
  index "idx_file_storages_expires_at" {
    columns = [column.expires_at]
  }
  index "idx_file_storages_organizer_id" {
    columns = [column.organizer_id]
  }
  index "idx_file_storages_uploaded_by" {
    columns = [column.uploaded_by]
  }
  index "idx_file_storages_user_id" {
    columns = [column.user_id]
  }
}
table "guest_users" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "email" {
    null = false
    type = character_varying(255)
  }
  column "first_name" {
    null = true
    type = character_varying(100)
  }
  column "last_name" {
    null = true
    type = character_varying(100)
  }
  column "phone" {
    null = true
    type = character_varying(20)
  }
  column "country_code" {
    null = true
    type = character_varying(5)
  }
  column "email_verified" {
    null    = true
    type    = boolean
    default = false
  }
  column "verification_token" {
    null = true
    type = character_varying(255)
  }
  column "token_expires_at" {
    null = true
    type = timestamptz
  }
  column "converted_to_user" {
    null    = true
    type    = boolean
    default = false
  }
  column "converted_user_id" {
    null = true
    type = uuid
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_guest_users_converted_user_id" {
    columns = [column.converted_user_id]
  }
  index "idx_guest_users_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_guest_users_email" {
    columns = [column.email]
  }
  index "idx_guest_users_verification_token" {
    columns = [column.verification_token]
  }
}
table "individual_tickets" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "ticket_id" {
    null = false
    type = uuid
  }
  column "ticket_number" {
    null = false
    type = character_varying(50)
  }
  column "qr_code" {
    null = true
    type = character_varying(500)
  }
  column "status" {
    null    = false
    type    = text
    default = "active"
  }
  column "check_in_time" {
    null = true
    type = timestamptz
  }
  column "check_out_time" {
    null = true
    type = timestamptz
  }
  column "checked_in_by" {
    null = true
    type = uuid
  }
  column "checked_out_by" {
    null = true
    type = uuid
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_individual_tickets_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_individual_tickets_ticket_id" {
    columns = [column.ticket_id]
  }
  index "idx_individual_tickets_ticket_number" {
    columns = [column.ticket_number]
  }
  unique "uni_individual_tickets_ticket_number" {
    columns = [column.ticket_number]
  }
}
table "invoices" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "invoice_number" {
    null = false
    type = character_varying(50)
  }
  column "transaction_id" {
    null = false
    type = uuid
  }
  column "customer_name" {
    null = false
    type = character_varying(255)
  }
  column "customer_email" {
    null = false
    type = character_varying(255)
  }
  column "customer_phone" {
    null = true
    type = character_varying(50)
  }
  column "amount" {
    null = false
    type = numeric(10,2)
  }
  column "currency" {
    null = false
    type = character_varying(3)
  }
  column "tax_amount" {
    null    = true
    type    = numeric(10,2)
    default = 0
  }
  column "total_amount" {
    null = false
    type = numeric(10,2)
  }
  column "file_url" {
    null = true
    type = text
  }
  column "file_key" {
    null = true
    type = text
  }
  column "receipt_url" {
    null = true
    type = text
  }
  column "status" {
    null    = false
    type    = character_varying(50)
    default = "generated"
  }
  column "sent_at" {
    null = true
    type = timestamptz
  }
  column "viewed_at" {
    null = true
    type = timestamptz
  }
  column "metadata" {
    null = true
    type = jsonb
  }
  column "issued_at" {
    null = false
    type = timestamptz
  }
  column "due_date" {
    null = true
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_invoices_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_invoices_transaction_id" {
    columns = [column.transaction_id]
  }
  unique "uni_invoices_invoice_number" {
    columns = [column.invoice_number]
  }
  unique "uni_invoices_transaction_id" {
    columns = [column.transaction_id]
  }
}
table "ledger_entries" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "organizer_id" {
    null = false
    type = uuid
  }
  column "transaction_id" {
    null = true
    type = uuid
  }
  column "type" {
    null = false
    type = character_varying(20)
  }
  column "amount_local" {
    null = false
    type = numeric(10,2)
  }
  column "currency" {
    null = false
    type = character_varying(3)
  }
  column "amount_base" {
    null = false
    type = numeric(10,2)
  }
  column "base_currency" {
    null    = true
    type    = character_varying(3)
    default = "USD"
  }
  column "exchange_rate" {
    null = false
    type = numeric(10,6)
  }
  column "description" {
    null = true
    type = character_varying(255)
  }
  column "metadata" {
    null = true
    type = jsonb
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_ledger_entries_organizer_id" {
    columns = [column.organizer_id]
  }
  index "idx_ledger_entries_transaction_id" {
    columns = [column.transaction_id]
  }
  index "idx_ledger_entries_type" {
    columns = [column.type]
  }
}
table "organizations" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "name" {
    null = false
    type = text
  }
  column "description" {
    null = true
    type = text
  }
  column "logo_url" {
    null = true
    type = text
  }
  column "website_url" {
    null = true
    type = text
  }
  column "organizer_id" {
    null = true
    type = uuid
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_organizations_deleted_at" {
    columns = [column.deleted_at]
  }
}
table "organizer_onboardings" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "organizer_id" {
    null = true
    type = uuid
  }
  column "is_profile_complete" {
    null    = false
    type    = boolean
    default = false
  }
  column "is_business_info_complete" {
    null    = false
    type    = boolean
    default = false
  }
  column "is_categories_selected" {
    null    = false
    type    = boolean
    default = false
  }
  column "is_onboarding_complete" {
    null    = false
    type    = boolean
    default = false
  }
  column "business_name" {
    null = true
    type = character_varying(200)
  }
  column "business_description" {
    null = true
    type = text
  }
  column "business_logo_url" {
    null = true
    type = character_varying(500)
  }
  column "business_website_url" {
    null = true
    type = character_varying(500)
  }
  column "business_email" {
    null = true
    type = character_varying(200)
  }
  column "business_phone" {
    null = true
    type = character_varying(50)
  }
  column "business_address" {
    null = true
    type = text
  }
  column "business_categories" {
    null = true
    type = sql("text[]")
  }
  column "years_of_experience" {
    null    = true
    type    = bigint
    default = 0
  }
  column "specialties" {
    null = true
    type = sql("text[]")
  }
  column "services_offered" {
    null = true
    type = sql("text[]")
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  column "is_complete" {
    null    = false
    type    = boolean
    default = false
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_organizer_onboardings_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_organizer_onboardings_organizer_id" {
    columns = [column.organizer_id]
  }
  unique "uni_organizer_onboardings_organizer_id" {
    columns = [column.organizer_id]
  }
}
table "organizer_tier_templates" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "organizer_id" {
    null = false
    type = uuid
  }
  column "template_name" {
    null = false
    type = character_varying(100)
  }
  column "description" {
    null = true
    type = text
  }
  column "is_active" {
    null    = false
    type    = boolean
    default = true
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_organizer_template_name" {
    unique  = true
    columns = [column.organizer_id, column.template_name]
    where   = "(deleted_at IS NULL)"
  }
  index "idx_organizer_tier_templates_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_organizer_tier_templates_organizer_id" {
    columns = [column.organizer_id]
  }
}
table "otps" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "identifier" {
    null = false
    type = text
  }
  column "type" {
    null = false
    type = text
  }
  column "role" {
    null    = false
    type    = text
    default = "user"
  }
  column "code" {
    null = false
    type = text
  }
  column "expires_at" {
    null = false
    type = timestamptz
  }
  column "attempts" {
    null    = true
    type    = bigint
    default = 0
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_otp_identifier_type_role" {
    columns = [column.identifier, column.type, column.role]
  }
  index "idx_otps_expires_at" {
    columns = [column.expires_at]
  }
}
table "payment_attempts" {
  schema = schema.public
  column "id" {
    null = false
    type = text
    default = sql("gen_random_uuid()")
  }
  column "payment_intent_id" {
    null = true
    type = text
  }
  column "provider" {
    null = true
    type = text
  }
  column "provider_reference_id" {
    null = true
    type = text
  }
  column "provider_session_id" {
    null = true
    type = text
  }
  column "provider_charge_id" {
    null = true
    type = text
  }
  column "amount" {
    null = true
    type = bigint
  }
  column "currency" {
    null = true
    type = text
  }
  column "status" {
    null = true
    type = text
  }
  column "payment_method_type" {
    null = true
    type = text
  }
  column "provider_data" {
    null = true
    type = jsonb
  }
  column "failure_reason" {
    null = true
    type = text
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
}
table "payment_audit_logs" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "action" {
    null = false
    type = character_varying(100)
  }
  column "entity_type" {
    null = false
    type = character_varying(50)
  }
  column "entity_id" {
    null = false
    type = uuid
  }
  column "actor_id" {
    null = true
    type = uuid
  }
  column "actor_type" {
    null = true
    type = character_varying(50)
  }
  column "event_id" {
    null = true
    type = uuid
  }
  column "changes_before" {
    null = true
    type = jsonb
  }
  column "changes_after" {
    null = true
    type = jsonb
  }
  column "ip_address" {
    null = true
    type = character_varying(45)
  }
  column "user_agent" {
    null = true
    type = text
  }
  column "metadata" {
    null = true
    type = jsonb
  }
  column "timestamp" {
    null = false
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_payment_audit_logs_action" {
    columns = [column.action]
  }
  index "idx_payment_audit_logs_actor_id" {
    columns = [column.actor_id]
  }
  index "idx_payment_audit_logs_entity_id" {
    columns = [column.entity_id]
  }
  index "idx_payment_audit_logs_entity_type" {
    columns = [column.entity_type]
  }
  index "idx_payment_audit_logs_timestamp" {
    columns = [column.timestamp]
  }
}
table "payment_bills" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "bill_number" {
    null = false
    type = character_varying(50)
  }
  column "event_id" {
    null = false
    type = uuid
  }
  column "organizer_id" {
    null = false
    type = uuid
  }
  column "admin_id" {
    null = false
    type = uuid
  }
  column "payment_method" {
    null = true
    type = text
  }
  column "payment_ref" {
    null = true
    type = text
  }
  column "status" {
    null    = false
    type    = text
    default = "pending"
  }
  column "notes" {
    null = true
    type = text
  }
  column "bill_date" {
    null = true
    type = timestamptz
  }
  column "paid_date" {
    null = true
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  column "total_revenue" {
    null    = false
    type    = numeric
    default = 0
  }
  column "total_commission" {
    null    = false
    type    = numeric
    default = 0
  }
  column "organizer_earnings" {
    null    = false
    type    = numeric
    default = 0
  }
  column "billed_amount" {
    null    = false
    type    = numeric
    default = 0
  }
  column "paid_amount" {
    null    = false
    type    = numeric
    default = 0
  }
  column "remaining_amount" {
    null    = false
    type    = numeric
    default = 0
  }
  column "bill_type" {
    null    = false
    type    = text
    default = "auto_calculated"
  }
  column "priority" {
    null    = false
    type    = text
    default = "normal"
  }
  column "due_date" {
    null = true
    type = timestamptz
  }
  column "payment_screenshot_url" {
    null = true
    type = character_varying(500)
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_payment_bills_admin_id" {
    columns = [column.admin_id]
  }
  index "idx_payment_bills_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_payment_bills_event_id" {
    columns = [column.event_id]
  }
  index "idx_payment_bills_organizer_id" {
    columns = [column.organizer_id]
  }
  unique "uni_payment_bills_bill_number" {
    columns = [column.bill_number]
  }
}
table "payment_gateway_configs" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "gateway_name" {
    null = false
    type = character_varying(50)
  }
  column "display_name" {
    null = false
    type = character_varying(100)
  }
  column "is_enabled" {
    null    = true
    type    = boolean
    default = false
  }
  column "is_test_mode" {
    null    = true
    type    = boolean
    default = true
  }
  column "priority" {
    null    = true
    type    = bigint
    default = 0
  }
  column "supported_countries" {
    null = true
    type = sql("text[]")
  }
  column "supported_currencies" {
    null = true
    type = sql("text[]")
  }
  column "api_key" {
    null = true
    type = text
  }
  column "api_secret" {
    null = true
    type = text
  }
  column "webhook_secret" {
    null = true
    type = text
  }
  column "config" {
    null = true
    type = jsonb
  }
  column "percentage_fee" {
    null    = true
    type    = numeric(5,2)
    default = 0
  }
  column "fixed_fee" {
    null    = true
    type    = numeric(10,2)
    default = 0
  }
  column "min_amount" {
    null = true
    type = numeric(10,2)
  }
  column "max_amount" {
    null = true
    type = numeric(10,2)
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "api_key_encrypted" {
    null = true
    type = text
  }
  column "api_secret_encrypted" {
    null = true
    type = text
  }
  column "webhook_secret_encrypted" {
    null = true
    type = text
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_payment_gateway_configs_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_payment_gateway_configs_gateway_name" {
    columns = [column.gateway_name]
  }
  unique "uni_payment_gateway_configs_gateway_name" {
    columns = [columnpa.gateway_name]
  }
}
table "payment_histories" {
  schema = schema.public
  column "id" {
    null = false
    type = bigserial
  }
  column "payment_bill_id" {
    null = false
    type = uuid
  }
  column "amount" {
    null = false
    type = numeric
  }
  column "payment_method" {
    null = false
    type = text
  }
  column "payment_ref" {
    null = true
    type = text
  }
  column "payment_date" {
    null = true
    type = timestamptz
  }
  column "processed_by_id" {
    null = false
    type = uuid
  }
  column "notes" {
    null = true
    type = text
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "screenshot_url" {
    null = true
    type = character_varying(500)
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_payment_histories_payment_bill_id" {
    columns = [column.payment_bill_id]
  }
}
table "payment_intents" {
  schema = schema.public

  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }

  column "actor_id" {
    null = false
    type = uuid
  }

  column "actor_type" {
    null = false
    type = text
  }

  column "event_id" {
    null = false
    type = uuid
  }

  column "tier_id" {
    null = false
    type = uuid
  }

  column "customer_email" {
    null = false
    type = text
  }

  column "quantity" {
    null = false
    type = integer
  }

  column "amount_total" {
    null = false
    type = bigint
  }

  column "currency" {
    null = false
    type = text
  }

  column "status" {
    null = false
    type = text
  }

  column "session_id" {
    null = true
    type = text
  }

  column "checkout_token" {
    null = false
    type = text
  }

  column "idempotency_key" {
    null = false
    type = text
  }

  column "payment_gateway" {
    null = false
    type = text
  }

  column "timezone" {
    null = false
    type = text
  }

  column "gateway_metadata" {
    null = true
    type = jsonb
  }

  column "expires_at" {
    null = false
    type = timestamptz
  }

  column "succeeded_at" {
    null = true
    type = timestamptz
  }

  column "canceled_at" {
    null = true
    type = timestamptz
  }

  column "failed_at" {
    null = true
    type = timestamptz
  }

  column "created_at" {
    null = false
    type = timestamptz
  }

  column "updated_at" {
    null = false
    type = timestamptz
  }

  primary_key {
    columns = [column.id]
  }

  unique "uq_payment_intent_idempotency" {
    columns = [column.idempotency_key]
  }

  index "idx_pi_checkout_token" {
    columns = [column.checkout_token]
  }

  index "idx_pi_status" {
    columns = [column.status]
  }
}
table "payout_requests" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "request_number" {
    null = false
    type = character_varying(50)
  }
  column "organizer_id" {
    null = false
    type = uuid
  }
  column "event_id" {
    null = true
    type = uuid
  }
  column "amount" {
    null = false
    type = numeric
  }
  column "status" {
    null    = false
    type    = text
    default = "pending"
  }
  column "request_type" {
    null    = false
    type    = text
    default = "event_payout"
  }
  column "description" {
    null = true
    type = text
  }
  column "admin_notes" {
    null = true
    type = text
  }
  column "processed_by" {
    null = true
    type = uuid
  }
  column "processed_at" {
    null = true
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  column "payment_bill_id" {
    null = true
    type = uuid
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_payout_requests_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_payout_requests_event_id" {
    columns = [column.event_id]
  }
  index "idx_payout_requests_organizer_id" {
    columns = [column.organizer_id]
  }
  index "idx_payout_requests_payment_bill_id" {
    columns = [column.payment_bill_id]
  }
  unique "uni_payout_requests_request_number" {
    columns = [column.request_number]
  }
}
table "permissions" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "name" {
    null = false
    type = text
  }
  column "description" {
    null = true
    type = text
  }
  column "resource" {
    null = false
    type = text
  }
  column "action" {
    null = false
    type = text
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  unique "uni_permissions_name" {
    columns = [column.name]
  }
}
table "processing_locks" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "lock_key" {
    null = false
    type = character_varying(255)
  }
  column "lock_type" {
    null = false
    type = character_varying(50)
  }
  column "owner_id" {
    null = false
    type = character_varying(255)
  }
  column "expires_at" {
    null = false
    type = timestamp
  }
  column "created_at" {
    null    = true
    type    = timestamp
    default = sql("now()")
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_processing_locks_expires" {
    columns = [column.expires_at]
  }
  index "idx_processing_locks_key" {
    columns = [column.lock_key]
  }
  index "idx_processing_locks_type_expires" {
    columns = [column.lock_type, column.expires_at]
  }
  unique "processing_locks_lock_key_key" {
    columns = [column.lock_key]
  }
}
table "refund_status_history" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "refund_id" {
    null = false
    type = text
  }
  column "old_status" {
    null = true
    type = character_varying(50)
  }
  column "new_status" {
    null = false
    type = character_varying(50)
  }
  column "changed_by_id" {
    null = true
    type = uuid
  }
  column "changed_by_type" {
    null    = true
    type    = character_varying(50)
    default = "system"
  }
  column "remarks" {
    null = true
    type = text
  }
  column "metadata" {
    null = true
    type = jsonb
  }
  column "changed_at" {
    null = false
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_refund_status_history_changed_at" {
    columns = [column.changed_at]
  }
  index "idx_refund_status_history_changed_by_id" {
    columns = [column.changed_by_id]
  }
  index "idx_refund_status_history_refund_id" {
    columns = [column.refund_id]
  }
}
table "refunds" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "refund_number" {
    null = true
    type = text
  }
  column "transaction_id" {
    null = true
    type = text
  }
  column "payment_intent_id" {
    null = true
    type = text
  }
  column "payment_gateway" {
    null = false
    type = character_varying(50)
  }
  column "gateway_refund_id" {
    null = false
    type = character_varying(255)
  }
  column "amount" {
    null = true
    type = bigint
  }
  column "currency" {
    null = true
    type = text
  }
  column "exchange_rate" {
    null    = true
    type    = numeric(10,6)
    default = 1
  }
  column "base_currency" {
    null    = true
    type    = character_varying(3)
    default = "USD"
  }
  column "base_currency_amount" {
    null = true
    type = numeric(10,2)
  }
  column "reason" {
    null = true
    type = text
  }
  column "refund_type" {
    null = false
    type = character_varying(50)
  }
  column "status" {
    null = true
    type = text
  }
  column "affected_ticket_ids" {
    null = true
    type = jsonb
  }
  column "ticket_count" {
    null = false
    type = bigint
  }
  column "commission_refund" {
    null = true
    type = numeric(10,2)
  }
  column "organizer_refund" {
    null = true
    type = numeric(10,2)
  }
  column "gateway_fee_refund" {
    null = true
    type = numeric(10,2)
  }
  column "initiated_by" {
    null = true
    type = uuid
  }
  column "approved_by" {
    null = true
    type = uuid
  }
  column "rejection_reason" {
    null = true
    type = text
  }
  column "gateway_response" {
    null = true
    type = jsonb
  }
  column "gateway_metadata" {
    null = true
    type = jsonb
  }
  column "metadata" {
    null = true
    type = jsonb
  }
  column "notes" {
    null = true
    type = text
  }
  column "requested_at" {
    null = true
    type = timestamptz
  }
  column "approved_at" {
    null = true
    type = timestamptz
  }
  column "processed_at" {
    null = true
    type = timestamptz
  }
  column "failed_at" {
    null = true
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  column "is_full_transaction_refund" {
    null    = true
    type    = boolean
    default = false
  }
  column "event_id" {
    null = true
    type = text
  }
  column "actor_id" {
    null = true
    type = text
  }
  column "actor_type" {
    null = true
    type = text
  }
  column "approved_by_id" {
    null = true
    type = text
  }
  column "approved_by_type" {
    null = true
    type = text
  }
  column "provider" {
    null = true
    type = text
  }
  column "provider_refund_id" {
    null = true
    type = text
  }
  column "is_full_refund" {
    null = true
    type = boolean
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_refunds_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_refunds_gateway_refund_id" {
    columns = [column.gateway_refund_id]
  }
  index "idx_refunds_payment_gateway" {
    columns = [column.payment_gateway]
  }
  index "idx_refunds_payment_intent_id" {
    columns = [column.payment_intent_id]
  }
  index "idx_refunds_status" {
    columns = [column.status]
  }
  index "idx_refunds_transaction_id" {
    columns = [column.transaction_id]
  }
}
table "registration_requests" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "email" {
    null = false
    type = text
  }
  column "first_name" {
    null = false
    type = text
  }
  column "last_name" {
    null = false
    type = text
  }
  column "phone" {
    null = false
    type = text
  }
  column "country_code" {
    null = false
    type = text
  }
  column "password" {
    null = false
    type = text
  }
  column "user_type" {
    null = false
    type = text
  }
  column "is_verified" {
    null    = true
    type    = boolean
    default = false
  }
  column "expires_at" {
    null = false
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_registration_requests_email" {
    unique  = true
    columns = [column.email]
  }
}
table "role_permissions" {
  schema = schema.public
  column "permission_id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "role_id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  primary_key {
    columns = [column.permission_id, column.role_id]
  }
}
table "roles" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "name" {
    null = false
    type = text
  }
  column "description" {
    null = true
    type = text
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  unique "uni_roles_name" {
    columns = [column.name]
  }
}
table "schema_migrations" {
  schema = schema.public
  column "version" {
    null = false
    type = bigint
  }
  column "dirty" {
    null = false
    type = boolean
  }
  primary_key {
    columns = [column.version]
  }
}
table "ticket_reservations" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "checkout_token" {
    null = false
    type = character_varying(255)
  }
  column "event_id" {
    null = false
    type = uuid
  }
  column "tier_id" {
    null = false
    type = uuid
  }
  column "user_id" {
    null = true
    type = uuid
  }
  column "guest_user_id" {
    null = true
    type = uuid
  }
  column "customer_email" {
    null = false
    type = character_varying(255)
  }
  column "quantity" {
    null = false
    type = integer
  }
  column "status" {
    null    = true
    type    = character_varying(20)
    default = "reserved"
  }
  column "expires_at" {
    null = false
    type = timestamp
  }
  column "confirmed_at" {
    null = true
    type = timestamp
  }
  column "created_at" {
    null    = true
    type    = timestamp
    default = sql("now()")
  }
  column "updated_at" {
    null    = true
    type    = timestamp
    default = sql("now()")
  }
  primary_key {
    columns = [column.id]
  }
  foreign_key "fk_ticket_reservations_event" {
    columns     = [column.event_id]
    ref_columns = [table.events.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "fk_ticket_reservations_guest" {
    columns     = [column.guest_user_id]
    ref_columns = [table.guest_users.column.id]
    on_update   = NO_ACTION
    on_delete   = SET_NULL
  }
  foreign_key "fk_ticket_reservations_tier" {
    columns     = [column.tier_id]
    ref_columns = [table.event_tiers.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "fk_ticket_reservations_user" {
    columns     = [column.user_id]
    ref_columns = [table.users.column.id]
    on_update   = NO_ACTION
    on_delete   = SET_NULL
  }
  index "idx_ticket_reservations_checkout_token" {
    columns = [column.checkout_token]
  }
  index "idx_ticket_reservations_event_tier" {
    columns = [column.event_id, column.tier_id]
  }
  index "idx_ticket_reservations_expires_at" {
    columns = [column.expires_at]
  }
  index "idx_ticket_reservations_status_expires" {
    columns = [column.status, column.expires_at]
  }
}
table "tickets" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "ticket_number" {
    null = false
    type = character_varying(50)
  }
  column "user_id" {
    null = true
    type = uuid
  }
  column "event_id" {
    null = false
    type = uuid
  }
  column "quantity" {
    null    = false
    type    = bigint
    default = 1
  }
  column "total_amount" {
    null = false
    type = numeric
  }
  column "status" {
    null    = false
    type    = text
    default = "ACTIVE"
  }
  column "check_in_time" {
    null = true
    type = timestamptz
  }
  column "check_out_time" {
    null = true
    type = timestamptz
  }
  column "checked_in_by" {
    null = true
    type = uuid
  }
  column "checked_out_by" {
    null = true
    type = uuid
  }
  column "purchase_date" {
    null = true
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  column "guest_user_id" {
    null = true
    type = uuid
  }
  column "is_guest_purchase" {
    null    = true
    type    = boolean
    default = false
  }
  column "checked_in_count" {
    null    = true
    type    = bigint
    default = 0
  }
  column "tier_id" {
    null = false
    type = uuid
  }
  column "payment_gateway" {
    null = false
    type = text
  }
  column "transaction_id" {
    null = true
    type = uuid
  }
  column "payment_status" {
    null    = true
    type    = character_varying(20)
    default = "PENDING"
  }
  column "paid_at" {
    null = true
    type = timestamp
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_tickets_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_tickets_event_id" {
    columns = [column.event_id]
  }
  index "idx_tickets_guest_user_id" {
    columns = [column.guest_user_id]
  }
  index "idx_tickets_paid_at" {
    columns = [column.paid_at]
    where   = "(paid_at IS NOT NULL)"
  }
  index "idx_tickets_payment_status" {
    columns = [column.payment_status]
  }
  index "idx_tickets_tier_id" {
    columns = [column.tier_id]
  }
  index "idx_tickets_transaction_id" {
    columns = [column.transaction_id]
  }
  index "idx_tickets_user_id" {
    columns = [column.user_id]
  }
  unique "uni_tickets_ticket_number" {
    columns = [column.ticket_number]
  }
}
table "tokens" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "user_id" {
    null = true
    type = uuid
  }
  column "token_hash" {
    null = false
    type = text
  }
  column "type" {
    null = false
    type = text
  }
  column "expires_at" {
    null = false
    type = timestamptz
  }
  column "revoked" {
    null    = true
    type    = boolean
    default = false
  }
  column "device" {
    null = true
    type = text
  }
  column "ip" {
    null = true
    type = text
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_tokens_user_id" {
    columns = [column.user_id]
  }
}
table "transactions" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "event_id" {
    null = true
    type = uuid
  }
  column "user_id" {
    null = true
    type = uuid
  }
  column "guest_user_id" {
    null = true
    type = uuid
  }
  column "payment_gateway" {
    null = false
    type = text
  }
  column "amount" {
    null = false
    type = numeric
  }
  column "currency" {
    null = true
    type = text
  }
  column "quantity" {
    null = true
    type = bigint
  }
  column "status" {
    null = true
    type = text
  }
  column "gateway_txn_id" {
    null = true
    type = text
  }
  column "gateway_data" {
    null = true
    type = jsonb
  }
  column "commission_rate" {
    null = false
    type = numeric
  }
  column "commission_amount" {
    null = false
    type = numeric
  }
  column "organizer_share" {
    null = false
    type = numeric
  }
  column "processed_at" {
    null = true
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  column "tier_id" {
    null = true
    type = uuid
  }
  column "payment_intent_id" {
    null = true
    type = text
  }
  column "amount_local" {
    null = true
    type = numeric(10,2)
  }
  column "amount_base" {
    null = true
    type = numeric(10,2)
  }
  column "exchange_rate" {
    null = true
    type = numeric(10,6)
  }
  column "platform_fee" {
    null = true
    type = bigint
  }
  column "base_currency" {
    null    = true
    type    = character_varying(3)
    default = "USD"
  }
  column "provider" {
    null = true
    type = text
  }
  column "stripe_payment_intent_id" {
    null = true
    type = character_varying(255)
  }
  column "stripe_charge_id" {
    null = true
    type = character_varying(255)
  }
  column "stripe_fee" {
    null = true
    type = numeric(10,2)
  }
  column "provider_txn_id" {
    null = true
    type = text
  }
  column "refund_status" {
    null    = false
    type    = character_varying(20)
    default = "NONE"
  }
  column "payment_attempt_id" {
    null = true
    type = text
  }
  column "actor_id" {
    null = true
    type = text
  }
  column "actor_type" {
    null = true
    type = text
  }
  column "amount_total" {
    null = true
    type = bigint
  }
  column "gateway_fee" {
    null = true
    type = bigint
  }
  column "organizer_earning" {
    null = true
    type = bigint
  }
  column "is_paid_out" {
    null = true
    type = boolean
  }
  column "paid_out_at" {
    null = true
    type = timestamptz
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_transactions_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_transactions_event_id" {
    columns = [column.event_id]
  }
  index "idx_transactions_guest_user_id" {
    columns = [column.guest_user_id]
  }
  index "idx_transactions_payment_intent_id" {
    columns = [column.payment_intent_id]
  }
  index "idx_transactions_provider" {
    columns = [column.provider]
  }
  index "idx_transactions_provider_txn_id" {
    columns = [column.provider_txn_id]
  }
  index "idx_transactions_refund_status" {
    columns = [column.refund_status]
  }
  index "idx_transactions_status" {
    columns = [column.status]
  }
  index "idx_transactions_stripe_charge_id" {
    columns = [column.stripe_charge_id]
  }
  index "idx_transactions_stripe_payment_intent_id" {
    columns = [column.stripe_payment_intent_id]
  }
  index "idx_transactions_tier_id" {
    columns = [column.tier_id]
  }
  index "idx_transactions_user_id" {
    columns = [column.user_id]
  }
}
table "user_roles" {
  schema = schema.public
  column "user_id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "role_id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  primary_key {
    columns = [column.user_id, column.role_id]
  }
}
table "users" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("public.uuid_generate_v4()")
  }
  column "email" {
    null = false
    type = text
  }
  column "password_hash" {
    null = false
    type = text
  }
  column "first_name" {
    null = true
    type = text
  }
  column "last_name" {
    null = true
    type = text
  }
  column "phone" {
    null = true
    type = text
  }
  column "is_email_verified" {
    null    = true
    type    = boolean
    default = false
  }
  column "verification_code" {
    null = true
    type = text
  }
  column "organization_id" {
    null = true
    type = uuid
  }
  column "created_by" {
    null = true
    type = uuid
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  column "country_code" {
    null = true
    type = text
  }
  column "organizer_status" {
    null    = true
    type    = text
    default = "inactive"
  }
  column "account_status" {
    null    = true
    type    = text
    default = "active"
  }
  column "admin_remark" {
    null = true
    type = text
  }
  column "approved_at" {
    null = true
    type = timestamptz
  }
  column "rejected_at" {
    null = true
    type = timestamptz
  }
  column "organizer_id" {
    null = true
    type = uuid
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_users_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_users_organization_id" {
    columns = [column.organization_id]
  }
  index "idx_users_organizer_id" {
    columns = [column.organizer_id]
  }
  unique "uni_users_email" {
    columns = [column.email]
  }
}
table "webhook_events" {
  schema = schema.public
  column "id" {
    null    = false
    type    = uuid
    default = sql("gen_random_uuid()")
  }
  column "payment_gateway" {
    null = false
    type = character_varying(50)
  }
  column "gateway_event_id" {
    null = false
    type = character_varying(255)
  }
  column "event_type" {
    null = true
    type = text
  }
  column "api_version" {
    null = true
    type = character_varying(50)
  }
  column "status" {
    null = true
    type = text
  }
  column "processed_count" {
    null    = true
    type    = bigint
    default = 0
  }
  column "last_error" {
    null = true
    type = text
  }
  column "payment_intent_id" {
    null = true
    type = text
  }
  column "transaction_id" {
    null = true
    type = text
  }
  column "refund_id" {
    null = true
    type = text
  }
  column "payload" {
    null = true
    type = jsonb
  }
  column "headers" {
    null = true
    type = jsonb
  }
  column "received_at" {
    null = true
    type = timestamptz
  }
  column "processed_at" {
    null = true
    type = timestamptz
  }
  column "created_at" {
    null = true
    type = timestamptz
  }
  column "deleted_at" {
    null = true
    type = timestamptz
  }
  column "updated_at" {
    null = true
    type = timestamptz
  }
  column "provider" {
    null = true
    type = text
  }
  column "event_id" {
    null = true
    type = text
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_webhook_events_deleted_at" {
    columns = [column.deleted_at]
  }
  index "idx_webhook_events_event_type" {
    columns = [column.event_type]
  }
  index "idx_webhook_events_gateway_event_id" {
    columns = [column.gateway_event_id]
  }
  index "idx_webhook_events_payment_gateway" {
    columns = [column.payment_gateway]
  }
  index "idx_webhook_events_received_at" {
    columns = [column.received_at]
  }
  index "idx_webhook_events_status" {
    columns = [column.status]
  }
}
schema "public" {
  comment = "standard public schema"
}
