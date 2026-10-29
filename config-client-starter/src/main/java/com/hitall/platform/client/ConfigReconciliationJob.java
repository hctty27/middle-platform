package com.hitall.platform.client;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Scheduled;

/**
 * 定期主动校对配置版本。
 * 即使 Webhook 通知丢失或暂时失败，也可通过周期同步恢复一致性。
 */
public class ConfigReconciliationJob {
    private static final Logger LOGGER = LoggerFactory.getLogger(ConfigReconciliationJob.class);
    private final ConfigSyncTrigger trigger;

    public ConfigReconciliationJob(ConfigSyncTrigger trigger) {
        this.trigger = trigger;
    }

    // 默认每 5 分钟执行一次；启动后延迟 3 秒首次执行。
    @Scheduled(fixedDelayString = "${config.client.reconcile-ms:300000}", initialDelay = 3000)
    public void reconcile() {
        try {
            trigger.requestSync();
        } catch (RuntimeException ex) {
            LOGGER.warn("Scheduling config reconciliation failed: {}", ex.getMessage());
        }
    }
}
