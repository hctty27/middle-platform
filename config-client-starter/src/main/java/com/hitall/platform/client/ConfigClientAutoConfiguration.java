package com.hitall.platform.client;

import org.springframework.boot.autoconfigure.AutoConfiguration;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.scheduling.annotation.EnableScheduling;

/**
 * 配置客户端 Starter 的 Spring Boot 自动装配入口。
 * 仅在 config.client.enabled=true 时注册同步、回调和定时补偿组件。
 */
@AutoConfiguration
@EnableScheduling
@EnableConfigurationProperties(ConfigClientProperties.class)
@ConditionalOnProperty(prefix = "config.client", name = "enabled", havingValue = "true")
public class ConfigClientAutoConfiguration {
    @Bean
    ClientSynchronizer clientSynchronizer(JdbcTemplate jdbc, ConfigClientProperties properties) {
        return new ClientSynchronizer(jdbc, properties);
    }

    @Bean
    ConfigCallbackController configCallbackController(ClientSynchronizer synchronizer,
                                                      ConfigClientProperties properties) {
        return new ConfigCallbackController(synchronizer, properties);
    }

    @Bean
    ConfigReconciliationJob configReconciliationJob(ClientSynchronizer synchronizer) {
        return new ConfigReconciliationJob(synchronizer);
    }
}
