package com.hitall.platform.client;

import java.util.concurrent.Executor;
import java.util.concurrent.atomic.AtomicBoolean;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

/**
 * 将 Webhook 与定时校对合并为单飞同步任务。
 * 同一 JVM 最多运行一个任务；执行期间的新通知合并为一次补充同步。
 * 任务失败后由后续 Webhook 或定时校对再次触发。
 */
public class ConfigSyncTrigger {
    private static final Logger LOGGER = LoggerFactory.getLogger(ConfigSyncTrigger.class);

    private final ClientSynchronizer synchronizer;
    private final Executor executor;
    private final AtomicBoolean pending = new AtomicBoolean();
    private final AtomicBoolean running = new AtomicBoolean();

    public ConfigSyncTrigger(ClientSynchronizer synchronizer, Executor executor) {
        this.synchronizer = synchronizer;
        this.executor = executor;
    }

    /** 只提交任务，不在 HTTP 请求线程中访问远端配置中心。 */
    public void requestSync() {
        pending.set(true);
        submitIfIdle();
    }

    private void submitIfIdle() {
        if (!running.compareAndSet(false, true)) return;
        try {
            executor.execute(this::drain);
        } catch (RuntimeException ex) {
            running.set(false);
            throw new ResponseStatusException(HttpStatus.SERVICE_UNAVAILABLE,
                    "Config sync executor is unavailable", ex);
        }
    }

    private void drain() {
        try {
            while (pending.getAndSet(false)) {
                try {
                    synchronizer.synchronize();
                } catch (RuntimeException ex) {
                    LOGGER.warn("Asynchronous config sync failed; scheduled reconciliation will retry", ex);
                    break;
                }
            }
        } finally {
            running.set(false);
            // 接近退出时的新通知不能丢失：重新竞争单飞执行权。
            if (pending.get()) submitIfIdle();
        }
    }
}
