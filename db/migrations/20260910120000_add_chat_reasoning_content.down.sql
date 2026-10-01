-- 仅在所有实例回退旧版本、备份该列且确认不再需要思考历史后执行。
-- 日常应用回滚应保留列，避免丢失协议回放数据。
ALTER TABLE chat_messages DROP COLUMN reasoning_content;
