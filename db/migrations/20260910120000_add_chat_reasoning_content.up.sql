-- MySQL 8.0.12+；先执行增量迁移再更新应用。旧版本可忽略新增列。
ALTER TABLE chat_messages
    ADD COLUMN reasoning_content LONGTEXT NULL,
    ALGORITHM=INSTANT;
