// Package pools holds reusable buffers for building SQL and URLs.
package pools

import (
	"strings"
	"sync"
)

type builderPool struct{ pool sync.Pool }

// BuilderPool hands out strings.Builders; Reset and Put them back when done.
var BuilderPool = &builderPool{pool: sync.Pool{New: func() any { return new(strings.Builder) }}}

func (p *builderPool) Get() *strings.Builder {
	sb, _ := p.pool.Get().(*strings.Builder)
	return sb
}

func (p *builderPool) Put(sb *strings.Builder) {
	sb.Reset()
	p.pool.Put(sb)
}
