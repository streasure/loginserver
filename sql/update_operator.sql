-- 已建表环境的增量更新：白/黑名单增加操作者审计字段
-- 新环境直接执行 create_tables.sql（已包含 operator 列），无需本脚本
USE `loginserver`;

ALTER TABLE `whitelist_ip`
    ADD COLUMN `operator` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '操作者' AFTER `remark`;

ALTER TABLE `blacklist_ip`
    ADD COLUMN `operator` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '操作者' AFTER `remark`;
