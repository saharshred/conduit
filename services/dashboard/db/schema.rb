# This file is auto-generated from the current state of the database. Instead
# of editing this file, please use the migrations feature of Active Record to
# incrementally modify your database, and then regenerate this schema definition.
#
# This file is the source Rails uses to define your schema when running `bin/rails
# db:schema:load`. When creating a new database, `bin/rails db:schema:load` tends to
# be faster and is potentially less error prone than running all of your
# migrations from scratch. Old migrations may fail to apply correctly if those
# migrations use external dependencies or application code.
#
# It's strongly recommended that you check this file into your version control system.

ActiveRecord::Schema[8.1].define(version: 2026_08_27_192336) do
  # These are extensions that must be enabled in order to support this database
  enable_extension "pg_catalog.plpgsql"

  create_table "flagged_transactions", force: :cascade do |t|
    t.string "account_id", null: false
    t.bigint "amount_cents", null: false
    t.datetime "created_at", null: false
    t.string "idempotency_key", null: false
    t.string "merchant_category", null: false
    t.datetime "occurred_at", null: false
    t.text "reasons", null: false
    t.string "status", default: "pending", null: false
    t.datetime "updated_at", null: false
    t.index ["idempotency_key"], name: "index_flagged_transactions_on_idempotency_key", unique: true
    t.index ["status"], name: "index_flagged_transactions_on_status"
  end

  create_table "transactions", force: :cascade do |t|
    t.text "account_id", null: false
    t.bigint "amount_cents", null: false
    t.text "bank", null: false
    t.text "currency", null: false
    t.text "idempotency_key", null: false
    t.timestamptz "ingested_at", default: -> { "now()" }, null: false
    t.text "merchant_category", null: false
    t.text "merchant_name", null: false
    t.text "native_id", null: false
    t.timestamptz "occurred_at", null: false
    t.index ["account_id", "occurred_at"], name: "transactions_account_idx"
    t.unique_constraint ["idempotency_key"], name: "transactions_idempotency_key_key"
  end
end
