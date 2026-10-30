-- 0023_notifications.down.sql
--
-- Reverts the PR4 notifications table. It is a standalone table with no
-- foreign keys, so a single drop removes it.
DROP TABLE IF EXISTS notifications;
