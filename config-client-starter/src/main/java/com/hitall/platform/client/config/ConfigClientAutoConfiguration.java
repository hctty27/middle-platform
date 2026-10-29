package com.hitall.platform.client.config;

import com.hitall.platform.client.callback.ConfigCallbackController;
import com.hitall.platform.client.sync.ClientSynchronizer;
import com.hitall.platform.client.sync.ConfigReconciliationJob;
import com.hitall.platform.client.sync.ConfigSyncTrigger;

import org.springframework.boot.autoconfigure.AutoConfiguration;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.scheduling.concurrent.ThreadPoolTaskExecutor;
import org.springframework.transaction.PlatformTransactionManager;
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
    ClientSynchronizer clientSynchronizer(JdbcTemplate jdbc, ConfigClientProperties properties,
                                          PlatformTransactionManager transactionManager) {
        return new ClientSynchronizer(jdbc, properties, transactionManager);
    }

    @Bean("configSyncExecutor")
    ThreadPoolTaskExecutor configSyncExecutor() {
        ThreadPoolTaskExecutor executor = new ThreadPoolTaskExecutor();
        executor.setCorePoolSize(1);
        executor.setMaxPoolSize(1);
        executor.setQueueCapacity(1);
        executor.setThreadNamePrefix("config-sync-");
        executor.setWaitForTasksToCompleteOnShutdown(true);
        executor.setAwaitTerminationSeconds(30);
        return executor;
    }

    @Bean
    ConfigSyncTrigger configSyncTrigger(ClientSynchronizer synchronizer,
                                        @Qualifier("configSyncExecutor") ThreadPoolTaskExecutor executor) {
        return new ConfigSyncTrigger(synchronizer, executor);
    }

    @Bean
    ConfigCallbackController configCallbackController(ConfigSyncTrigger trigger,
                                                      ConfigClientProperties properties) {
        return new ConfigCallbackController(trigger, properties);
    }

    @Bean
    ConfigReconciliationJob configReconciliationJob(ConfigSyncTrigger trigger) {
        return new ConfigReconciliationJob(trigger);
    }
}
