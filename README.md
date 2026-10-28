# Middle Platform · 研发中台

配置中心采用 JDK 25 + Spring Boot 4 + Gradle 9、单 MySQL 权威库、无状态多实例、Webhook 轻量版本通知、客户端按需拉取变更节点。

管理端采用 Vue 3，不提供登录、账号和角色权限功能。**管理 API 必须只允许通过可信内网或受控网关访问，严禁无防护公开暴露。**

模块规划：`config-center-server`、`config-center-admin`、`config-client-starter`、`config-client-demo`。
