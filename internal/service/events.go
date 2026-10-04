package service

import (
    "context"
    "database/sql"
    "encoding/json"
    "strings"
)

// Event is the API representation of an audit event.
type Event struct {
    ID          int64           `json:"id"`
    OccurredAt  string          `json:"occurred_at"`
    Actor       *Ref            `json:"actor"`
    Action      string          `json:"action"`
    SubjectType string          `json:"subject_type"`
    SubjectID   int64           `json:"subject_id"`
    Data        json.RawMessage `json:"data"`
    RequestID   string          `json:"request_id"`
}

// EventFilter controls the global activity feed.
type EventFilter struct {
    ActorID     *int64
    Action      string // exact, or a prefix ending in "." such as "stock."
    SubjectType string
    SubjectID   *int64
    From        string
    To          string
    Paging
}

const eventCols = `e.id, e.occurred_at, e.actor_user_id, u.display_name, e.action, e.subject_type, e.subject_id, e.data_json, e.request_id`

func scanEvents(rows *sql.Rows) ([]Event, error) {
    defer rows.Close()
    out := []Event{}
    for rows.Next() {
        var e Event
        var actorID sql.NullInt64
        var actorName sql.NullString
        var data string
        if err := rows.Scan(&e.ID, &e.OccurredAt, &actorID, &actorName, &e.Action, &e.SubjectType, &e.SubjectID,
            &data, &e.RequestID); err != nil {
            return nil, err
        }
        if actorID.Valid {
            e.Actor = &Ref{actorID.Int64, actorName.String}
        }
        e.Data = json.RawMessage(data)
        out = append(out, e)
    }
    return out, rows.Err()
}

// SubjectEvents returns the history of one entity, newest first.
func (s *Service) SubjectEvents(ctx context.Context, subjectType string, id int64, pg Paging) (*Page[Event], error) {
    pg = pg.Normalise()
    out := &Page[Event]{}
    if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_subjects WHERE subject_type = ? AND subject_id = ?`,
        subjectType, id).Scan(&out.Total); err != nil {
        return nil, err
    }
    rows, err := s.DB.R.QueryContext(ctx, `SELECT `+eventCols+`
        FROM event_subjects es
        JOIN events e ON e.id = es.event_id
        LEFT JOIN users u ON u.id = e.actor_user_id
        WHERE es.subject_type = ? AND es.subject_id = ?
        ORDER BY e.id DESC LIMIT ? OFFSET ?`, subjectType, id, pg.Limit, pg.Offset)
    if err != nil {
        return nil, err
    }
    out.Items, err = scanEvents(rows)
    return out, err
}

// ListEvents returns the global activity feed.
func (s *Service) ListEvents(ctx context.Context, f EventFilter) (*Page[Event], error) {
    f.Paging = f.Paging.Normalise()
    where := []string{"1 = 1"}
    var args []any
    if f.ActorID != nil {
        where = append(where, "e.actor_user_id = ?")
        args = append(args, *f.ActorID)
    }
    if f.Action != "" {
        if strings.HasSuffix(f.Action, ".") {
            where = append(where, "e.action LIKE ? ESCAPE '\\'")
            args = append(args, escapeLike(f.Action)+"%")
        } else {
            where = append(where, "e.action = ?")
            args = append(args, f.Action)
        }
    }
    if f.SubjectType != "" && f.SubjectID != nil {
        where = append(where, "e.id IN (SELECT event_id FROM event_subjects WHERE subject_type = ? AND subject_id = ?)")
        args = append(args, f.SubjectType, *f.SubjectID)
    } else if f.SubjectType != "" {
        where = append(where, "e.subject_type = ?")
        args = append(args, f.SubjectType)
    }
    if f.From != "" {
        where = append(where, "e.occurred_at >= ?")
        args = append(args, f.From)
    }
    if f.To != "" {
        where = append(where, "e.occurred_at < ?")
        args = append(args, f.To)
    }
    w := strings.Join(where, " AND ")
    out := &Page[Event]{}
    if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM events e WHERE `+w, args...).Scan(&out.Total); err != nil {
        return nil, err
    }
    rows, err := s.DB.R.QueryContext(ctx, `SELECT `+eventCols+` FROM events e
        LEFT JOIN users u ON u.id = e.actor_user_id
        WHERE `+w+` ORDER BY e.id DESC LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
    if err != nil {
        return nil, err
    }
    out.Items, err = scanEvents(rows)
    return out, err
}
