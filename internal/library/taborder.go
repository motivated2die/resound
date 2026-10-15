package library

import "slices"

// The phone can arrange its tabs (edit mode, long press and drag). The
// arrangement is stored once for all phones (tabOrder in library.json) and
// applies to the phone only: Tabs keeps the folder order the panel shows,
// PhoneTabs applies the arrangement.
//
// The stored order is a preference, not a list of the tabs, so it never
// needs to be kept in step with the folders:
//   - Tabs whose folder is gone are skipped, but keep their place in the
//     order while they are tombstoned: a folder removed and added back (or
//     moved out and back) returns to where it was. Renamed folders keep
//     their tab ID and so their place.
//   - Tabs the order doesn't know (a new subfolder, an added folder) go
//     right after the tab before them in folder order, so a new subfolder
//     appears next to its parent folder or its siblings rather than at the
//     end. New tabs in a row keep their folder order. Scans write them into
//     the order (arrangeTabsLocked), so it always holds every known tab where
//     the phone shows it, and a tab that goes and comes back finds its place
//     among them.
//   - Tabs whose tombstone expires leave the order.

// PhoneTabs returns all present tabs with their present sounds, in the
// phone's order.
func (l *Library) PhoneTabs() []Tab {
	l.mu.RLock()
	defer l.mu.RUnlock()
	ids := l.phoneOrderLocked()
	out := make([]Tab, 0, len(ids))
	for _, id := range ids {
		out = append(out, l.tabLocked(id))
	}
	return out
}

// arrangeTabsLocked brings a stored order up to date after a scan: tabs that
// no longer exist (expired tombstones) leave it, new tabs are placed. It
// reports whether the order changed. Without an arrangement there is
// nothing to do: the phone shows the folder order.
func (l *Library) arrangeTabsLocked() bool {
	if len(l.tabOrder) == 0 {
		return false
	}
	known := make([]uint64, 0, len(l.tabOrder))
	for _, id := range l.tabOrder {
		if l.tabs[id] != nil {
			known = append(known, id)
		}
	}
	present := func(id uint64) bool { return l.tabs[id].MissingSince == nil }
	next := mergeAfter(known, l.order, present)
	if slices.Equal(next, l.tabOrder) {
		return false
	}
	l.tabOrder = next
	return true
}

// phoneOrderLocked is the present tabs' IDs in the phone's order.
func (l *Library) phoneOrderLocked() []uint64 {
	if len(l.tabOrder) == 0 {
		return slices.Clone(l.order)
	}
	present := make(map[uint64]bool, len(l.order))
	for _, id := range l.order {
		present[id] = true
	}
	known := make([]uint64, 0, len(l.order))
	for _, id := range l.tabOrder {
		if present[id] {
			known = append(known, id)
		}
	}
	return mergeAfter(known, l.order, func(id uint64) bool { return true })
}

// SetTabOrder stores the phone's arrangement of the present tabs (tab IDs,
// first to last) and returns the order the phone now shows. Unknown and
// repeated IDs are ignored. Tabs the list lacks (tombstoned ones, or new
// ones the phone hadn't seen yet) keep their place after the tab they
// followed so far.
func (l *Library) SetTabOrder(ids []uint64) ([]uint64, error) {
	l.mu.Lock()
	seen := make(map[uint64]bool, len(ids))
	order := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if t := l.tabs[id]; t != nil && t.MissingSince == nil && !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
	}
	prev := l.tabOrder
	if len(prev) == 0 {
		prev = l.order
	}
	order = mergeAfter(order, prev, func(id uint64) bool { return l.tabs[id] != nil })
	if len(order) == 0 {
		order = nil
	}
	changed := !slices.Equal(order, l.tabOrder)
	l.tabOrder = order
	shown := l.phoneOrderLocked()
	if changed {
		l.revision++
	}
	l.mu.Unlock()
	if !changed {
		return shown, nil
	}
	return shown, l.saver.schedule()
}

// mergeAfter returns base with the IDs of ref that base lacks and add
// accepts, each inserted right after the nearest ID before it in ref that is
// in base, or first when there is none. Inserted IDs keep their order from
// ref. Linear, as /api/tabs calls it on every request.
func mergeAfter(base, ref []uint64, add func(uint64) bool) []uint64 {
	in := make(map[uint64]bool, len(base))
	for _, id := range base {
		in[id] = true
	}
	var front []uint64
	after := map[uint64][]uint64{} // base ID -> IDs inserted after it
	var anchor uint64
	anchored := false
	for _, id := range ref {
		switch {
		case in[id]:
			anchor, anchored = id, true
		case !add(id):
		case anchored:
			after[anchor] = append(after[anchor], id)
		default:
			front = append(front, id)
		}
	}
	out := append(make([]uint64, 0, len(base)+len(ref)), front...)
	for _, id := range base {
		out = append(out, id)
		out = append(out, after[id]...)
	}
	return out
}
