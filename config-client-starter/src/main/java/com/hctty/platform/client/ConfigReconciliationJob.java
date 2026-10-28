package com.hctty.platform.client;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Scheduled;

/**
 * 定期主动校对配置版本。
 * 即使 Webhook 通知丢失或暂时失败，也可通过周期同步恢复一致性。
 */
public class ConfigReconciliationJob {
    private static final Logger LOGGER = LoggerFactory.getLogger(ConfigReconciliationJob.class);
    private final ClientSynchronizer synchronizer;

    public ConfigReconciliationJob(ClientSynchronizer synchronizer) {
        this.synchronizer = synchronizer;
    }

    // 默认每 5 分钟执行一次；启动后延迟 3 秒首次执行。
    @Scheduled(fixedDelayString = "${config.client.reconcile-ms:300000}", initialDelay = 3000)
    public void reconcile() {
        try {
            synchronizer.synchronize();
        } catch (RuntimeException ex) {
            LOGGER.warn("Config reconciliation failed: {}", ex.getMessage());
        }
    }
}
