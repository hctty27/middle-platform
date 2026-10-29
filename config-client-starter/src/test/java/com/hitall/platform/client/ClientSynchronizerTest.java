package com.hitall.platform.client;

import java.util.List;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.InOrder;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.TransactionDefinition;
import org.springframework.transaction.TransactionStatus;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.*;

class ClientSynchronizerTest {
    private LocalConfigRepository local;
    private ConfigCenterRemoteClient remote;
    private ClientSynchronizer synchronizer;

    @BeforeEach
    void setUp() {
        local = mock(LocalConfigRepository.class);
        remote = mock(ConfigCenterRemoteClient.class);
        PlatformTransactionManager transactionManager = mock(PlatformTransactionManager.class);
        when(transactionManager.getTransaction(any(TransactionDefinition.class)))
                .thenReturn(mock(TransactionStatus.class));
        synchronizer = new ClientSynchronizer(local, remote, transactionManager);
    }

    @Test
    void fetchesRemotelyBeforeLockingAndCommitsCursorWithNodeChanges() {
        var before = new ClientSynchronizer.State("epoch-a", 0);
        var after = new ClientSynchronizer.State("epoch-a", 1);
        when(local.readState()).thenReturn(before, after);
        when(local.lockedState()).thenReturn(before);
        when(remote.revision()).thenReturn(new ClientSynchronizer.Revision("epoch-a", 1));
        when(remote.changes(0, 200)).thenReturn(new ClientSynchronizer.Event[] {
                new ClientSynchronizer.Event(1, 42, "DELETE", null)
        });
        when(remote.batchGet(List.of(42L))).thenReturn(new ClientSynchronizer.Node[0]);

        synchronizer.synchronize();

        InOrder ordered = inOrder(local, remote);
        ordered.verify(local).readState();
        ordered.verify(remote).revision();
        ordered.verify(remote).changes(0, 200);
        ordered.verify(remote).batchGet(List.of(42L));
        ordered.verify(remote).revision();
        ordered.verify(local).lockedState();
        ordered.verify(local).delete(42L);
        ordered.verify(local).moveCursor("epoch-a", 1);
    }

    @Test
    void neverOverwritesAnotherInstancesMoreRecentCursor() {
        var before = new ClientSynchronizer.State("epoch-a", 0);
        var after = new ClientSynchronizer.State("epoch-a", 1);
        when(local.readState()).thenReturn(before, after);
        when(local.lockedState()).thenReturn(after);
        when(remote.revision()).thenReturn(new ClientSynchronizer.Revision("epoch-a", 1));
        when(remote.changes(0, 200)).thenReturn(new ClientSynchronizer.Event[] {
                new ClientSynchronizer.Event(1, 42, "DELETE", null)
        });
        when(remote.batchGet(List.of(42L))).thenReturn(new ClientSynchronizer.Node[0]);

        synchronizer.synchronize();

        verify(local, never()).delete(anyLong());
        verify(local, never()).moveCursor(any(), anyLong());
    }

    @Test
    void restoresSnapshotOnlyAfterCheckingLocalCursor() {
        var before = new ClientSynchronizer.State(null, 0);
        var after = new ClientSynchronizer.State("epoch-a", 5);
        var snapshot = new ClientSynchronizer.Snapshot("epoch-a", 5, List.of());
        when(local.readState()).thenReturn(before, after);
        when(local.lockedState()).thenReturn(before);
        when(remote.revision()).thenReturn(new ClientSynchronizer.Revision("epoch-a", 5));
        when(remote.snapshot()).thenReturn(snapshot);

        synchronizer.synchronize();

        InOrder ordered = inOrder(remote, local);
        ordered.verify(local).readState();
        ordered.verify(remote).revision();
        ordered.verify(remote).snapshot();
        ordered.verify(local).lockedState();
        ordered.verify(local).replaceSnapshot(snapshot);
    }

    @Test
    void resetsFromSnapshotWhenIncrementalHistoryHasGap() {
        var before = new ClientSynchronizer.State("epoch-a", 0);
        var after = new ClientSynchronizer.State("epoch-a", 5);
        var snapshot = new ClientSynchronizer.Snapshot("epoch-a", 5, List.of());
        when(local.readState()).thenReturn(before, after);
        when(local.lockedState()).thenReturn(before);
        when(remote.revision()).thenReturn(new ClientSynchronizer.Revision("epoch-a", 5));
        when(remote.changes(0, 200)).thenReturn(new ClientSynchronizer.Event[] {
                new ClientSynchronizer.Event(2, 42, "UPSERT", null)
        });
        when(remote.snapshot()).thenReturn(snapshot);

        synchronizer.synchronize();

        verify(remote, never()).batchGet(any());
        verify(local).replaceSnapshot(snapshot);
    }
    @Test
    void networkFailureDoesNotWriteNodesOrAdvanceCursor() {
        var current = new ClientSynchronizer.State("epoch-a", 7);
        when(local.readState()).thenReturn(current);
        when(remote.revision()).thenThrow(new org.springframework.web.client.ResourceAccessException("timeout"));

        org.junit.jupiter.api.Assertions.assertThrows(
                org.springframework.web.client.ResourceAccessException.class, synchronizer::synchronize);

        verify(local, never()).lockedState();
        verify(local, never()).moveCursor(any(), anyLong());
        verify(local, never()).delete(anyLong());
        verify(local, never()).upsert(any());
    }

}
