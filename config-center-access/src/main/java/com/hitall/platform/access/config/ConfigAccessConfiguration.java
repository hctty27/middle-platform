package com.hitall.platform.access.config;

import com.hitall.platform.access.sync.ClientSynchronizer;
import com.hitall.platform.access.sync.ConfigReconciliationJob;
import com.hitall.platform.access.sync.ConfigSyncTrigger;

import org.springframework.context.annotation.Configuration;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.scheduling.concurrent.ThreadPoolTaskExecutor;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.scheduling.annotation.EnableScheduling;

/**
 * 独立接入工程的 Spring Bean 配置。
 * Webhook Controller 由组件扫描注册，避免重复 Bean 与请求映射。
 */
@Configuration(proxyBeanMethods = false)
@EnableScheduling
@EnableConfigurationProperties(ConfigClientProperties.class)
public class ConfigAccessConfiguration {
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
    ConfigReconciliationJob configReconciliationJob(ConfigSyncTrigger trigger) {
        return new ConfigReconciliationJob(trigger);
    }
}
