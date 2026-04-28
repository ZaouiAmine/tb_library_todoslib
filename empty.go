package lib

import (
	"encoding/json"
	"io"

	"github.com/taubyte/go-sdk/database"
	"github.com/taubyte/go-sdk/event"
	httpevent "github.com/taubyte/go-sdk/http/event"
)

const dbMatch = "/todos"
const stateKey = "todos"
const seqKey = "_seq"

type todo struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type mutateBody struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Text   string `json:"text"`
}

func parseSeq(b []byte) int {
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			continue
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func seqBytes(n int) []byte {
	if n == 0 {
		return []byte("0")
	}
	var buf [16]byte
	i := len(buf)
	for x := n; x > 0; {
		i--
		buf[i] = byte('0' + x%10)
		x /= 10
	}
	return buf[i:]
}

func nextTodoID(db database.Database) (string, error) {
	raw, _ := db.Get(seqKey)
	n := parseSeq(raw) + 1
	if err := db.Put(seqKey, seqBytes(n)); err != nil {
		return "", err
	}
	return "t" + string(seqBytes(n)), nil
}

func loadTodos(db database.Database) ([]todo, error) {
	raw, err := db.Get(stateKey)
	if err != nil || len(raw) == 0 {
		return []todo{}, nil
	}
	var list []todo
	if err := json.Unmarshal(raw, &list); err != nil {
		return []todo{}, nil
	}
	return list, nil
}

func saveTodos(db database.Database, list []todo) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return db.Put(stateKey, raw)
}

func writeErr(h httpevent.Event, msg string) {
	raw, _ := json.Marshal(map[string]string{"error": msg})
	_ = h.Headers().Set("Content-Type", "application/json")
	_ = h.Headers().Set("Access-Control-Allow-Origin", "*")
	_, _ = h.Write(raw)
	_ = h.Return(400)
}

func writeJSON(h httpevent.Event, status int, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		writeErr(h, "encode failed")
		return
	}
	_ = h.Headers().Set("Content-Type", "application/json")
	_ = h.Headers().Set("Access-Control-Allow-Origin", "*")
	_, _ = h.Write(raw)
	_ = h.Return(status)
}

//export ListTodos
func ListTodos(e event.Event) uint32 {
	h, err := e.HTTP()
	if err != nil {
		return 1
	}

	db, err := database.New(dbMatch)
	if err != nil {
		writeErr(h, "database open failed")
		return 1
	}
	defer db.Close()

	list, err := loadTodos(db)
	if err != nil {
		writeErr(h, "load failed")
		return 1
	}

	writeJSON(h, 200, list)
	return 0
}

//export MutateTodos
func MutateTodos(e event.Event) uint32 {
	h, err := e.HTTP()
	if err != nil {
		return 1
	}

	body, err := io.ReadAll(h.Body())
	if err != nil {
		writeErr(h, "read body")
		return 1
	}
	_ = h.Body().Close()

	var req mutateBody
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeErr(h, "invalid json")
			return 1
		}
	}

	db, err := database.New(dbMatch)
	if err != nil {
		writeErr(h, "database open failed")
		return 1
	}
	defer db.Close()

	list, err := loadTodos(db)
	if err != nil {
		writeErr(h, "load failed")
		return 1
	}

	switch req.Action {
	case "add":
		if req.Text == "" {
			writeErr(h, "text required")
			return 1
		}
		id, err := nextTodoID(db)
		if err != nil {
			writeErr(h, "id alloc failed")
			return 1
		}
		list = append(list, todo{ID: id, Text: req.Text, Done: false})
	case "toggle":
		if req.ID == "" {
			writeErr(h, "id required")
			return 1
		}
		for i := range list {
			if list[i].ID == req.ID {
				list[i].Done = !list[i].Done
				break
			}
		}
	case "delete":
		if req.ID == "" {
			writeErr(h, "id required")
			return 1
		}
		next := list[:0]
		for _, t := range list {
			if t.ID != req.ID {
				next = append(next, t)
			}
		}
		list = next
	default:
		writeErr(h, "unknown action")
		return 1
	}

	if err := saveTodos(db, list); err != nil {
		writeErr(h, "save failed")
		return 1
	}

	writeJSON(h, 200, list)
	return 0
}
