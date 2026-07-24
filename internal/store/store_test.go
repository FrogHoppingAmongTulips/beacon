package store

import "testing"

func TestUpdatePartial(t *testing.T) {
	s, err := Open(t.TempDir() + "/users.json")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Add("Артём", "iPhone")
	if err != nil {
		t.Fatal(err)
	}
	if !u.Enabled {
		t.Fatal("новый ключ должен быть включён")
	}

	// переименование не трогает Enabled и device
	name := "Артём-2"
	got, changed, err := s.Update(u.ID, &name, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("rename не должен менять enabled")
	}
	if got.Name != "Артём-2" || got.Device != "iPhone" {
		t.Fatalf("после rename: %+v", got)
	}

	// выключение
	off := false
	got, changed, err = s.Update(u.ID, nil, nil, &off)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || got.Enabled {
		t.Fatalf("должен был выключиться: changed=%v enabled=%v", changed, got.Enabled)
	}

	// повторное выключение — без изменения
	if _, changed, _ = s.Update(u.ID, nil, nil, &off); changed {
		t.Fatal("повторное выключение не должно считаться изменением")
	}

	// несуществующий id
	if _, _, err := s.Update("nope", &name, nil, nil); err == nil {
		t.Fatal("ждали ErrNotFound")
	}

	// изменения переживают перечитывание с диска
	s2, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := s2.Get(u.ID); r.Name != "Артём-2" || r.Enabled {
		t.Fatalf("после перезагрузки: %+v", r)
	}
}
