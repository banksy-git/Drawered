package service

import (
    "context"
    "database/sql"
    "strings"

    "drawered/internal/decimal"
)

// StockOp is a request to change stock levels.
type StockOp struct {
    PartID         int64         `json:"part_id"`
    LocationID     int64         `json:"location_id"`
    FromLocationID int64         `json:"from_location_id"`
    ToLocationID   int64         `json:"to_location_id"`
    Quantity       decimal.Milli `json:"quantity"`
    Reason         string        `json:"reason"`
}

type stockPart struct {
    Name            string
    UOM             string
    AllowFractional bool
}

func loadStockPart(ctx context.Context, tx *sql.Tx, id int64) (*stockPart, error) {
    p := &stockPart{}
    var deleted sql.NullString
    err := tx.QueryRowContext(ctx, `SELECT name, uom, allow_fractional, deleted_at FROM parts WHERE id = ?`, id).
        Scan(&p.Name, &p.UOM, &p.AllowFractional, &deleted)
    if err == sql.ErrNoRows {
        return nil, NotFound("part")
    }
    if err != nil {
        return nil, err
    }
    if deleted.Valid {
        return nil, Conflict("part_deleted", "part is deleted")
    }
    return p, nil
}

func currentQty(ctx context.Context, tx *sql.Tx, partID, locID int64) (decimal.Milli, bool, error) {
    var q decimal.Milli
    err := tx.QueryRowContext(ctx, `SELECT quantity_milli FROM stock WHERE part_id = ? AND location_id = ?`, partID, locID).Scan(&q)
    if err == sql.ErrNoRows {
        return 0, false, nil
    }
    return q, err == nil, err
}

func checkReason(r string) (string, error) {
    r = strings.TrimSpace(r)
    return r, checkLen("reason", r, 0, 500)
}

// StockAdjust applies add, remove or set at a single location.
func (s *Service) StockAdjust(ctx context.Context, kind string, op StockOp) (*Part, error) {
    reason, err := checkReason(op.Reason)
    if err != nil {
        return nil, err
    }
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        p, err := loadStockPart(ctx, tx, op.PartID)
        if err != nil {
            return err
        }
        if err := checkQuantity(op.Quantity, p.AllowFractional, kind == "set"); err != nil {
            return err
        }
        ix, err := loadLocations(ctx, tx)
        if err != nil {
            return err
        }
        if err := checkStockLocation(ix, op.LocationID); err != nil {
            return err
        }
        before, exists, err := currentQty(ctx, tx, op.PartID, op.LocationID)
        if err != nil {
            return err
        }
        var after decimal.Milli
        var action string
        switch kind {
        case "add":
            after, action = before+op.Quantity, "stock.added"
        case "remove":
            if !exists || op.Quantity > before {
                return &Error{Status: 409, Code: "insufficient_stock",
                    Message: "cannot remove more than is held at this location",
                    Details: map[string]any{"available": before}}
            }
            after, action = before-op.Quantity, "stock.removed"
        case "set":
            after, action = op.Quantity, "stock.set"
            if exists && after == before {
                return nil
            }
        default:
            return Invalid("invalid_operation", "unknown stock operation")
        }
        if exists {
            _, err = tx.ExecContext(ctx, `UPDATE stock SET quantity_milli = ? WHERE part_id = ? AND location_id = ?`,
                after, op.PartID, op.LocationID)
        } else {
            _, err = tx.ExecContext(ctx, `INSERT INTO stock (part_id, location_id, quantity_milli) VALUES (?, ?, ?)`,
                op.PartID, op.LocationID, after)
        }
        if err != nil {
            return err
        }
        if !exists {
            if err := reindexPart(ctx, tx, ix, op.PartID); err != nil {
                return err
            }
        }
        return s.event(ctx, tx, action, Subject{"part", op.PartID}, map[string]any{
            "part":     Ref{op.PartID, p.Name},
            "location": Ref{op.LocationID, ix.byID[op.LocationID].Name},
            "path":     ix.path(op.LocationID),
            "quantity": op.Quantity, "before": before, "after": after, "delta": after - before,
            "uom": p.UOM, "reason": reason,
        }, Subject{"location", op.LocationID})
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, op.PartID)
}

// StockMove moves a quantity of a part between two locations atomically.
func (s *Service) StockMove(ctx context.Context, op StockOp) (*Part, error) {
    reason, err := checkReason(op.Reason)
    if err != nil {
        return nil, err
    }
    if op.FromLocationID == op.ToLocationID {
        return nil, Invalid("same_location", "source and destination must differ")
    }
    err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
        p, err := loadStockPart(ctx, tx, op.PartID)
        if err != nil {
            return err
        }
        if err := checkQuantity(op.Quantity, p.AllowFractional, false); err != nil {
            return err
        }
        ix, err := loadLocations(ctx, tx)
        if err != nil {
            return err
        }
        if err := checkStockLocation(ix, op.FromLocationID); err != nil {
            return err
        }
        if err := checkStockLocation(ix, op.ToLocationID); err != nil {
            return err
        }
        fromQty, fromExists, err := currentQty(ctx, tx, op.PartID, op.FromLocationID)
        if err != nil {
            return err
        }
        if !fromExists || op.Quantity > fromQty {
            return &Error{Status: 409, Code: "insufficient_stock",
                Message: "cannot move more than is held at the source location",
                Details: map[string]any{"available": fromQty}}
        }
        toQty, toExists, err := currentQty(ctx, tx, op.PartID, op.ToLocationID)
        if err != nil {
            return err
        }
        if _, err := tx.ExecContext(ctx, `UPDATE stock SET quantity_milli = ? WHERE part_id = ? AND location_id = ?`,
            fromQty-op.Quantity, op.PartID, op.FromLocationID); err != nil {
            return err
        }
        if toExists {
            _, err = tx.ExecContext(ctx, `UPDATE stock SET quantity_milli = ? WHERE part_id = ? AND location_id = ?`,
                toQty+op.Quantity, op.PartID, op.ToLocationID)
        } else {
            _, err = tx.ExecContext(ctx, `INSERT INTO stock (part_id, location_id, quantity_milli) VALUES (?, ?, ?)`,
                op.PartID, op.ToLocationID, op.Quantity)
        }
        if err != nil {
            return err
        }
        if !toExists {
            if err := reindexPart(ctx, tx, ix, op.PartID); err != nil {
                return err
            }
        }
        return s.event(ctx, tx, "stock.moved", Subject{"part", op.PartID}, map[string]any{
            "part":          Ref{op.PartID, p.Name},
            "from_location": Ref{op.FromLocationID, ix.byID[op.FromLocationID].Name},
            "from_path":     ix.path(op.FromLocationID),
            "to_location":   Ref{op.ToLocationID, ix.byID[op.ToLocationID].Name},
            "to_path":       ix.path(op.ToLocationID),
            "quantity":      op.Quantity,
            "from_before":   fromQty, "from_after": fromQty - op.Quantity,
            "to_before": toQty, "to_after": toQty + op.Quantity,
            "uom": p.UOM, "reason": reason,
        }, Subject{"location", op.FromLocationID}, Subject{"location", op.ToLocationID})
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, op.PartID)
}

// EntryInput creates an empty stock entry.
type EntryInput struct {
    PartID      int64          `json:"part_id"`
    LocationID  int64          `json:"location_id"`
    MinQuantity *decimal.Milli `json:"min_quantity"`
    Note        string         `json:"note"`
}

// AddStockLocation creates an empty stock entry for a part at a location.
func (s *Service) AddStockLocation(ctx context.Context, in EntryInput) (*Part, error) {
    in.Note = strings.TrimSpace(in.Note)
    if err := checkLen("note", in.Note, 0, 200); err != nil {
        return nil, err
    }
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        p, err := loadStockPart(ctx, tx, in.PartID)
        if err != nil {
            return err
        }
        if in.MinQuantity != nil {
            if err := checkQuantity(*in.MinQuantity, p.AllowFractional, true); err != nil {
                return err
            }
        }
        ix, err := loadLocations(ctx, tx)
        if err != nil {
            return err
        }
        if err := checkStockLocation(ix, in.LocationID); err != nil {
            return err
        }
        if _, exists, err := currentQty(ctx, tx, in.PartID, in.LocationID); err != nil {
            return err
        } else if exists {
            return Conflict("stock_entry_exists", "the part is already stocked at that location")
        }
        if _, err := tx.ExecContext(ctx, `INSERT INTO stock (part_id, location_id, quantity_milli, min_quantity_milli, note)
                VALUES (?, ?, 0, ?, ?)`, in.PartID, in.LocationID, in.MinQuantity, in.Note); err != nil {
            return err
        }
        if err := reindexPart(ctx, tx, ix, in.PartID); err != nil {
            return err
        }
        return s.event(ctx, tx, "stock.location_added", Subject{"part", in.PartID}, map[string]any{
            "part":     Ref{in.PartID, p.Name},
            "location": Ref{in.LocationID, ix.byID[in.LocationID].Name},
            "path":     ix.path(in.LocationID),
        }, Subject{"location", in.LocationID})
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, in.PartID)
}

// UpdateStockEntry changes a stock entry's threshold and note.
func (s *Service) UpdateStockEntry(ctx context.Context, partID, locID int64, p Patch) (*Part, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        part, err := loadStockPart(ctx, tx, partID)
        if err != nil {
            return err
        }
        var min sql.NullInt64
        var note string
        err = tx.QueryRowContext(ctx, `SELECT min_quantity_milli, note FROM stock WHERE part_id = ? AND location_id = ?`,
            partID, locID).Scan(&min, &note)
        if err == sql.ErrNoRows {
            return NotFound("stock entry")
        }
        if err != nil {
            return err
        }
        var curMin *decimal.Milli
        if min.Valid {
            m := decimal.Milli(min.Int64)
            curMin = &m
        }
        nextMin, nextNote := curMin, note
        if p.Has("min_quantity") {
            nextMin = nil
            if !p.IsNull("min_quantity") {
                var m decimal.Milli
                if _, err := p.Get("min_quantity", &m); err != nil {
                    return err
                }
                if err := checkQuantity(m, part.AllowFractional, true); err != nil {
                    return err
                }
                nextMin = &m
            }
        }
        if _, err := p.Get("note", &nextNote); err != nil {
            return err
        }
        nextNote = strings.TrimSpace(nextNote)
        if err := checkLen("note", nextNote, 0, 200); err != nil {
            return err
        }
        ch := changes{}
        ch.add("min_quantity", fmtMilli(curMin), fmtMilli(nextMin))
        ch.add("note", note, nextNote)
        if len(ch) == 0 {
            return nil
        }
        if _, err := tx.ExecContext(ctx, `UPDATE stock SET min_quantity_milli = ?, note = ? WHERE part_id = ? AND location_id = ?`,
            nextMin, nextNote, partID, locID); err != nil {
            return err
        }
        ix, err := loadLocations(ctx, tx)
        if err != nil {
            return err
        }
        return s.event(ctx, tx, "stock.entry_updated", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, part.Name}, "location": Ref{locID, ix.byID[locID].Name},
            "path": ix.path(locID), "changes": ch,
        }, Subject{"location", locID})
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}

// RemoveStockLocation deletes a stock entry whose quantity is zero.
func (s *Service) RemoveStockLocation(ctx context.Context, partID, locID int64) (*Part, error) {
    err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
        p, err := loadStockPart(ctx, tx, partID)
        if err != nil {
            return err
        }
        q, exists, err := currentQty(ctx, tx, partID, locID)
        if err != nil {
            return err
        }
        if !exists {
            return NotFound("stock entry")
        }
        if q != 0 {
            return Conflict("stock_not_empty", "remove the stock before removing the location")
        }
        if _, err := tx.ExecContext(ctx, `DELETE FROM stock WHERE part_id = ? AND location_id = ?`, partID, locID); err != nil {
            return err
        }
        ix, err := loadLocations(ctx, tx)
        if err != nil {
            return err
        }
        if err := reindexPart(ctx, tx, ix, partID); err != nil {
            return err
        }
        return s.event(ctx, tx, "stock.location_removed", Subject{"part", partID}, map[string]any{
            "part": Ref{partID, p.Name}, "location": Ref{locID, ix.byID[locID].Name}, "path": ix.path(locID),
        }, Subject{"location", locID})
    })
    if err != nil {
        return nil, err
    }
    return s.GetPart(ctx, partID)
}
