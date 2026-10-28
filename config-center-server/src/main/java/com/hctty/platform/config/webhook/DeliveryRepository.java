package com.hctty.platform.config.webhook;

import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;
import org.springframework.transaction.annotation.Transactional;

@Repository
public class DeliveryRepository {
    private final JdbcTemplate jdbc;

    public DeliveryRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    @Transactional
    public Optional<Delivery> claim() {
        List<Delivery> candidates = jdbc.query("""
                SELECT d.subscriber_id,s.callback_url,s.webhook_secret,r.epoch,r.revision
                FROM config_delivery_state d
                JOIN config_subscriber s ON s.id=d.subscriber_id
                JOIN config_revision r ON r.id=1
                WHERE s.enabled=1 AND d.acknowledged_revision<r.revision
                  AND d.next_attempt_at<=UTC_TIMESTAMP(6)
                  AND (d.lease_until IS NULL OR d.lease_until<UTC_TIMESTAMP(6))
                ORDER BY d.next_attempt_at,d.subscriber_id
                LIMIT 1 FOR UPDATE SKIP LOCKED
                """,
                (rs, row) -> new Delivery(rs.getLong(1), rs.getString(2), rs.getString(3),
                        rs.getString(4), rs.getLong(5), UUID.randomUUID().toString()));
        if (candidates.isEmpty()) return Optional.empty();
        Delivery delivery = candidates.getFirst();
        jdbc.update("UPDATE config_delivery_state SET lease_owner=?,lease_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND) " +
                "WHERE subscriber_id=?", delivery.leaseId(), delivery.subscriberId());
        return Optional.of(delivery);
    }

    public void delivered(Delivery delivery) {
        jdbc.update("""
                UPDATE config_delivery_state
                   SET acknowledged_revision=GREATEST(acknowledged_revision,?),
                       lease_owner=NULL,lease_until=NULL,failure_count=0,last_error=NULL,
                       next_attempt_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6)
                 WHERE subscriber_id=? AND lease_owner=?
                """, delivery.revision(), delivery.subscriberId(), delivery.leaseId());
    }

    public void failed(Delivery delivery, String error) {
        jdbc.update("""
                UPDATE config_delivery_state
                   SET lease_owner=NULL,lease_until=NULL,
                       failure_count=failure_count+1,
                       next_attempt_at=DATE_ADD(UTC_TIMESTAMP(6),
                           INTERVAL LEAST(300,POW(2,LEAST(failure_count+1,8))) SECOND),
                       last_error=?,updated_at=UTC_TIMESTAMP(6)
                 WHERE subscriber_id=? AND lease_owner=?
                """, error.substring(0, Math.min(500, error.length())),
                delivery.subscriberId(), delivery.leaseId());
    }

    public record Delivery(long subscriberId, String url, String secret,
                           String epoch, long revision, String leaseId) {}
}
