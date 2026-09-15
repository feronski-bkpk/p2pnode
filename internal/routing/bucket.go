package routing

type bucket struct {
	contacts []Contact
}

func newBucket() *bucket {
	return &bucket{contacts: make([]Contact, 0, 8)}
}

func (b *bucket) len() int { return len(b.contacts) }

func (b *bucket) find(id ID) int {
	for i, c := range b.contacts {
		if c.NodeID == id {
			return i
		}
	}
	return -1
}

func (b *bucket) update(c Contact) {
	idx := b.find(c.NodeID)
	if idx < 0 {
		return
	}
	copy(b.contacts[idx:], b.contacts[idx+1:])
	b.contacts[len(b.contacts)-1] = c
}

func (b *bucket) moveToTail(id ID) {
	idx := b.find(id)
	if idx < 0 {
		return
	}
	c := b.contacts[idx]
	copy(b.contacts[idx:], b.contacts[idx+1:])
	b.contacts[len(b.contacts)-1] = c
}

func (b *bucket) appendTail(c Contact) {
	b.contacts = append(b.contacts, c)
}

func (b *bucket) removeHead() {
	if len(b.contacts) == 0 {
		return
	}
	copy(b.contacts, b.contacts[1:])
	b.contacts = b.contacts[:len(b.contacts)-1]
}

func (b *bucket) head() (Contact, bool) {
	if len(b.contacts) == 0 {
		return Contact{}, false
	}
	return b.contacts[0], true
}

func (b *bucket) snapshot() []Contact {
	out := make([]Contact, len(b.contacts))
	copy(out, b.contacts)
	return out
}
