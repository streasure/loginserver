USE `loginserver`;

CREATE TABLE IF NOT EXISTS `whitelist_ip` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `ip`         VARCHAR(45)     NOT NULL COMMENT 'IPv4/IPv6',
    `remark`     VARCHAR(255)    NOT NULL DEFAULT '' COMMENT '备注',
    `operator`   VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '操作者',
    `created_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_ip` (`ip`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci COMMENT ='IP白名单：登录检测放行，跳过黑名单';

CREATE TABLE IF NOT EXISTS `blacklist_ip` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `ip`         VARCHAR(45)     NOT NULL COMMENT 'IPv4/IPv6',
    `remark`     VARCHAR(255)    NOT NULL DEFAULT '' COMMENT '备注',
    `operator`   VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '操作者',
    `created_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_ip` (`ip`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci COMMENT ='IP黑名单：登录检测拦截';

CREATE TABLE IF NOT EXISTS `banned_role` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `role_id`    VARCHAR(64)     NOT NULL COMMENT '角色ID（accountId）',
    `reason`     VARCHAR(255)    NOT NULL DEFAULT '' COMMENT '封禁原因',
    `operator`   VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '操作者',
    `created_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_role` (`role_id`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci COMMENT ='封禁角色表：登录检测拦截';

CREATE TABLE IF NOT EXISTS `server_info` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `server_id`   VARCHAR(64)     NOT NULL COMMENT '服务器ID',
    `server_name` VARCHAR(128)    NOT NULL DEFAULT '' COMMENT '服务器名称',
    `zone`        VARCHAR(32)     NOT NULL DEFAULT '' COMMENT '区服',
    `ip`          VARCHAR(45)     NOT NULL DEFAULT '' COMMENT '接入地址',
    `port`        INT             NOT NULL DEFAULT 0 COMMENT '接入端口',
    `status`      TINYINT         NOT NULL DEFAULT 1 COMMENT '状态：0维护 1开放',
    `sort`        INT             NOT NULL DEFAULT 0 COMMENT '排序（小在前）',
    `created_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_server_id` (`server_id`)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_general_ci COMMENT ='服务器信息列表';
