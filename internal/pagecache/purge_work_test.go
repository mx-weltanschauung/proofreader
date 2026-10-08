package pagecache

import "testing"

// PurgeWork заведён под снятие тома: строка работы исчезает, и адресно сбросить
// её главы уже нечем — DELETE /works/{id}/chapters/{id}/cache резолвит главу из
// базы, а строки нет. Без этого снятый том жил бы в кэше до часа.
func TestPurgeWorkRemovesOnlyThatWork(t *testing.T) {
	s := New(t.TempDir())
	doomed := []Key{
		{WorkID: 47, Start: 1, End: 100},
		{WorkID: 47, Start: 101, End: 200},
	}
	neighbour := Key{WorkID: 48, Start: 1, End: 100}

	for _, k := range doomed {
		if err := s.Put(k, raw); err != nil {
			t.Fatalf("Put %+v: %v", k, err)
		}
	}
	if err := s.Put(neighbour, raw); err != nil {
		t.Fatalf("Put соседа: %v", err)
	}

	files, bytes, err := s.PurgeWork(47)
	if err != nil {
		t.Fatalf("PurgeWork: %v", err)
	}
	if files != len(doomed) {
		t.Errorf("PurgeWork унёс %d файлов, ожидалось %d", files, len(doomed))
	}
	if bytes <= 0 {
		t.Errorf("PurgeWork отчитался о %d байтах", bytes)
	}
	for _, k := range doomed {
		if _, _, ok := s.Open(k); ok {
			t.Errorf("файл %+v остался после PurgeWork", k)
		}
	}
	if _, _, ok := s.Open(neighbour); !ok {
		t.Error("PurgeWork задел кэш соседнего тома")
	}
}

// Номер тома — не префикс имени каталога: w4 и w47 лежат рядом, и снятие
// четвёртого не должно уносить сорок седьмой.
func TestPurgeWorkDoesNotMatchByPrefix(t *testing.T) {
	s := New(t.TempDir())
	short := Key{WorkID: 4, Start: 1, End: 10}
	long := Key{WorkID: 47, Start: 1, End: 10}
	if err := s.Put(short, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(long, raw); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, _, err := s.PurgeWork(4); err != nil {
		t.Fatalf("PurgeWork: %v", err)
	}
	if _, _, ok := s.Open(long); !ok {
		t.Error("PurgeWork(4) унёс кэш тома 47")
	}
}

func TestPurgeWorkOfUncachedWorkIsNotAnError(t *testing.T) {
	s := New(t.TempDir())
	files, bytes, err := s.PurgeWork(999)
	if err != nil {
		t.Errorf("PurgeWork несуществующего вернул ошибку: %v", err)
	}
	if files != 0 || bytes != 0 {
		t.Errorf("PurgeWork отчитался о %d файлах и %d байтах на пустом кэше", files, bytes)
	}
}

func TestPurgeWorkOnDisabledStoreIsSilent(t *testing.T) {
	s := New("")
	files, bytes, err := s.PurgeWork(1)
	if err != nil || files != 0 || bytes != 0 {
		t.Errorf("PurgeWork на выключенном хранилище: %d, %d, %v", files, bytes, err)
	}
}
