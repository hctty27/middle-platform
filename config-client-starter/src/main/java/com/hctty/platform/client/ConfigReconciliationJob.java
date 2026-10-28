package com.hctty.platform.client;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Scheduled;

public class ConfigReconciliationJob {
    private static final Logger LOGGER = LoggerFactory.getLogger(ConfigReconciliationJob.class);
    private final ClientSynchronizer synchronizer;

    public ConfigReconciliationJob(ClientSynchronizer synchronizer) {
        this.synchronizer = synchronizer;
    }

    @Scheduled(fixedDelayString = "${config.client.reconcile-ms:300000}", initialDelay = 3000)
    public void reconcile() {
        try {
            synchronizer.synchronize();
        } catch (RuntimeException ex) {
            LOGGER.warn("Config reconciliation failed: {}", ex.getMessage());
        }
    }
}
