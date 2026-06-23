// This file is a thin test-only wrapper around (*v772.Protocol).WriteMapChunk. The real chunk wire encoding lives in internal/protocol/v772/chunk_map.go. Kept as a forwarder so existing tests can keep using (*Player).BuildChunkPacketForTest.

package player

import (
	"goore/internal/world"
)

// BuildChunkPacketForTest builds a complete map_chunk (0x27) packet for a *world.Chunk. Test-only forwarder. The pos parameter is accepted for source compatibility but the chunk's own Pos field is what actually goes on the wire.
func (p *Player) BuildChunkPacketForTest(c *world.Chunk, pos world.ChunkPos) []byte {
	return p.Proto.WriteMapChunk(c)
}
