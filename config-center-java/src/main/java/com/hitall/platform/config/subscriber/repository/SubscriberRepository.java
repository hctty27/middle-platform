package com.hitall.platform.config.subscriber.repository;

import java.util.List;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;
import com.hitall.platform.config.subscriber.dto.CreateSubscriber;
import com.hitall.platform.config.subscriber.dto.SubscriberView;

/** 持久化订阅者及其投递游标；由业务 Service 提供写事务。 */
@Repository
public class SubscriberRepository {
    private final JdbcTemplate jdbc;

    public SubscriberRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public List<SubscriberView> list() {
        return jdbc.query("""
                SELECT s.id,s.code,s.callback_url,s.enabled,d.acknowledged_revision,
                       d.failure_count,d.last_error
                FROM config_subscriber s JOIN config_delivery_state d ON d.subscriber_id=s.id
                ORDER BY s.id
                """,
                (rs, row) -> new SubscriberView(rs.getLong(1), rs.getString(2),
                        rs.getString(3), rs.getBoolean(4), rs.getLong(5),
                        rs.getInt(6), rs.getString(7)));
    }

    public void create(CreateSubscriber request) {
        jdbc.update("INSERT INTO config_subscriber(code,callback_url,webhook_secret,enabled,created_at,updated_at) " +
                        "VALUES(?,?,?,1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))",
                request.code(), request.callbackUrl(), request.secret());
        Long id = jdbc.queryForObject("SELECT LAST_INSERT_ID()", Long.class);
        // 为新订阅者建立独立游标，后续由投递任务判断是否有未通知版本。
        jdbc.update("INSERT INTO config_delivery_state(subscriber_id,next_attempt_at,updated_at) " +
                "VALUES(?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", id);
    }
}
