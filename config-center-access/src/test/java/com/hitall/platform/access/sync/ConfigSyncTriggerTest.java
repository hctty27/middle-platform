package com.hitall.platform.access.sync;

import java.util.ArrayDeque;
import java.util.Queue;
import java.util.concurrent.Executor;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

class ConfigSyncTriggerTest {

    private static final class ManualExecutor implements Executor {
        private final Queue<Runnable> tasks = new ArrayDeque<>();

        @Override
        public void execute(Runnable command) {
            tasks.add(command);
        }

        void runNext() {
            tasks.remove().run();
        }

        int queued() {
            return tasks.size();
        }
    }

    @Test
    void returnsBeforeRunningSyncAndCoalescesRapidNotifications() {
        ClientSynchronizer synchronizer = mock(ClientSynchronizer.class);
        ManualExecutor executor = new ManualExecutor();
        ConfigSyncTrigger trigger = new ConfigSyncTrigger(synchronizer, executor);

        trigger.requestSync();
        trigger.requestSync();
        trigger.requestSync();

        assertEquals(1, executor.queued());
        verifyNoInteractions(synchronizer);

        executor.runNext();

        verify(synchronizer, times(1)).synchronize();
        assertEquals(0, executor.queued());
    }

    @Test
    void notificationDuringSyncCausesOneFollowUpSync() {
        ClientSynchronizer synchronizer = mock(ClientSynchronizer.class);
        ManualExecutor executor = new ManualExecutor();
        ConfigSyncTrigger trigger = new ConfigSyncTrigger(synchronizer, executor);
        doAnswer(invocation -> {
            trigger.requestSync();
            trigger.requestSync();
            return null;
        }).doNothing().when(synchronizer).synchronize();

        trigger.requestSync();
        executor.runNext();

        verify(synchronizer, times(2)).synchronize();
        assertEquals(0, executor.queued());
    }

    @Test
    void failureDoesNotBlockSubsequentReconciliation() {
        ClientSynchronizer synchronizer = mock(ClientSynchronizer.class);
        ManualExecutor executor = new ManualExecutor();
        ConfigSyncTrigger trigger = new ConfigSyncTrigger(synchronizer, executor);
        doThrow(new IllegalStateException("offline")).doNothing()
                .when(synchronizer).synchronize();

        trigger.requestSync();
        executor.runNext();
        trigger.requestSync();
        executor.runNext();

        verify(synchronizer, times(2)).synchronize();
        assertEquals(0, executor.queued());
    }

    @Test
    void rejectsTaskWhenExecutorCannotAcceptIt() {
        ClientSynchronizer synchronizer = mock(ClientSynchronizer.class);
        Executor executor = command -> { throw new java.util.concurrent.RejectedExecutionException(); };
        ConfigSyncTrigger trigger = new ConfigSyncTrigger(synchronizer, executor);

        org.springframework.web.server.ResponseStatusException error =
                assertThrows(org.springframework.web.server.ResponseStatusException.class, trigger::requestSync);
        assertEquals(org.springframework.http.HttpStatus.SERVICE_UNAVAILABLE, error.getStatusCode());
    }
}
