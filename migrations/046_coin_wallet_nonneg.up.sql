-- Floors on the xu wallet.
-- migrations/040_mascot_room_shared.up.sql already declares
--   balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0)
-- on databases that created user_coin_wallets from that file. That check is
-- unnamed in the file; Postgres stores it as user_coin_wallets_balance_check.
-- 045 added earned_balance and paid_balance with no floor.
--
-- Existing rows should already be >= 0. Credits only add, debits require
-- balance >= amount, and 045 copies balance into earned_balance with
-- paid_balance 0. This migration does not rewrite rows. If any wallet is
-- already negative, it stops and leaves the table unchanged.
--
-- Re-running is safe. A column that already has a >= 0 check is skipped,
-- including the balance check from 040.

DO $$
DECLARE
    bad bigint;
    def text;
    has_balance boolean := false;
    has_earned boolean := false;
    has_paid boolean := false;
    rec record;
BEGIN
    SELECT COUNT(*) INTO bad
    FROM user_coin_wallets
    WHERE balance < 0 OR earned_balance < 0 OR paid_balance < 0;
    IF bad > 0 THEN
        RAISE EXCEPTION 'user_coin_wallets has % row(s) below zero; not rewriting them', bad;
    END IF;

    FOR rec IN
        SELECT pg_get_constraintdef(oid) AS def
        FROM pg_constraint
        WHERE conrelid = 'user_coin_wallets'::regclass
          AND contype = 'c'
    LOOP
        def := regexp_replace(lower(rec.def), '[[:space:]]', '', 'g');
        IF def LIKE '%earned_balance>=0%' THEN
            has_earned := true;
        ELSIF def LIKE '%paid_balance>=0%' THEN
            has_paid := true;
        ELSIF def LIKE '%balance>=0%' THEN
            has_balance := true;
        END IF;
    END LOOP;

    IF NOT has_balance THEN
        BEGIN
            ALTER TABLE user_coin_wallets
                ADD CONSTRAINT user_coin_wallets_balance_nonneg CHECK (balance >= 0);
        EXCEPTION
            WHEN duplicate_object THEN NULL;
        END;
    END IF;
    IF NOT has_earned THEN
        BEGIN
            ALTER TABLE user_coin_wallets
                ADD CONSTRAINT user_coin_wallets_earned_balance_nonneg CHECK (earned_balance >= 0);
        EXCEPTION
            WHEN duplicate_object THEN NULL;
        END;
    END IF;
    IF NOT has_paid THEN
        BEGIN
            ALTER TABLE user_coin_wallets
                ADD CONSTRAINT user_coin_wallets_paid_balance_nonneg CHECK (paid_balance >= 0);
        EXCEPTION
            WHEN duplicate_object THEN NULL;
        END;
    END IF;
END $$;
