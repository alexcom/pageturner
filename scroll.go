package main

// scroller is a cursor over n rows, of which rows are visible at a time.
// offset is the first visible row and follows the cursor, so lists scroll
// line by line instead of jumping by pages.
type scroller struct {
	n, rows        int
	cursor, offset int
}

func (s *scroller) SetCount(n int) { s.n = n; s.clamp() }
func (s *scroller) SetRows(r int)  { s.rows = r; s.clamp() }
func (s *scroller) Move(d int)     { s.cursor += d; s.clamp() }
func (s *scroller) Select(i int)   { s.cursor = i; s.clamp() }
func (s *scroller) Top()           { s.Select(0) }
func (s *scroller) Bottom()        { s.Select(s.n - 1) }
func (s *scroller) Page() int      { return max(1, s.rows) }
func (s *scroller) Cursor() int    { return s.cursor }

// Visible returns the [start, end) range of rows to draw.
func (s *scroller) Visible() (int, int) {
	return s.offset, min(s.n, s.offset+max(1, s.rows))
}

func (s *scroller) clamp() {
	if s.n <= 0 {
		s.cursor, s.offset = 0, 0
		return
	}
	rows := max(1, s.rows)
	s.cursor = max(0, min(s.cursor, s.n-1))
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+rows {
		s.offset = s.cursor - rows + 1
	}
	s.offset = max(0, min(s.offset, s.n-rows))
}
