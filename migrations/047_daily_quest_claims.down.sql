-- Drops only the daily quest claim table. Wallet and ledger rows stay,
-- including credits already written for daily_quest_morning,
-- daily_quest_evening, and daily_quest_bonus.

DROP TABLE IF EXISTS daily_quest_claims;
