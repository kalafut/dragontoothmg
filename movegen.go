// dragontoothmg is a fast chess move generator library based on magic bitboards.
package dragontoothmg

// The main Dragontooth move generator file.
// Functions are in this file if (and only if) they are performance-critical
// move generator components, called while actually generating moves in-game.
// (The exception is a few one-line helpers for Move and Board in types.go)

import (
	"math/bits"
)

// The main API entrypoint. Generates all pseudo-legal moves for a given board.
// "Pseudo-legal moves" means that checking is ignored; generated moves might
// move into check, fail to break check, or castle through check.
func (b *Board) GenerateLegalMoves() []Move {
	moves := make([]Move, 0, 45)
	b.pawnPushes(&moves)
	b.pawnCaptures(&moves)
	b.knightMoves(&moves)
	b.kingMoves(&moves)
	b.rookMoves(&moves)
	b.bishopMoves(&moves)
	b.queenMoves(&moves)
	return moves
}

func (b *Board) pawnPushes(moveList *[]Move) {
	targets, doubleTargets := b.pawnPushBitboards()
	oneRankBack := 8
	if b.wtomove {
		oneRankBack = -oneRankBack
	}
	// push all pawns by one square
	for targets != 0 {
		target := bits.TrailingZeros64(targets)
		targets &= targets - 1 // unset the lowest active bit
		var canPromote bool
		if b.wtomove {
			canPromote = target >= 56
		} else {
			canPromote = target <= 7
		}
		var move Move
		move.Setfrom(Square(target + oneRankBack)).Setto(Square(target))
		if canPromote {
			for i := Piece(Knight); i <= Queen; i++ {
				move.Setpromote(i)
				*moveList = append(*moveList, move)
			}
		} else {
			*moveList = append(*moveList, move)
		}
	}
	// push some pawns by two squares
	for doubleTargets != 0 {
		doubleTarget := bits.TrailingZeros64(doubleTargets)
		doubleTargets &= doubleTargets - 1 // unset the lowest active bit
		var move Move
		move.Setfrom(Square(doubleTarget + 2*oneRankBack)).Setto(Square(doubleTarget))
		*moveList = append(*moveList, move)
	}
}

func (b *Board) pawnPushBitboards() (targets uint64, doubleTargets uint64) {
	free := (^b.white.all) & (^b.black.all)
	if b.wtomove {
		targets = b.white.pawns << 8 & free
		fourthFile := uint64(0xFF000000)
		doubleTargets = targets << 8 & fourthFile & free
	} else {
		targets = b.black.pawns >> 8 & free
		fifthFile := uint64(0xFF00000000)
		doubleTargets = targets >> 8 & fifthFile & free
	}
	return
}

func (b *Board) pawnCaptures(moveList *[]Move) {
	east, west := b.pawnCaptureBitboards()
	bitboards := [2]uint64{east, west}
	if !b.wtomove {
		bitboards[0], bitboards[1] = bitboards[1], bitboards[0]
	}
	for dir, board := range bitboards { // for east and west
		for board != 0 {
			target := bits.TrailingZeros64(board)
			board &= board - 1
			var move Move
			move.Setto(Square(target))
			canPromote := false
			if b.wtomove {
				move.Setfrom(Square(target - (9 - (dir * 2))))
				canPromote = target >= 56
			} else {
				move.Setfrom(Square(target + (9 - (dir * 2))))
				canPromote = target <= 7
			}
			if canPromote {
				for i := Piece(Knight); i <= Queen; i++ {
					move.Setpromote(i)
					*moveList = append(*moveList, move)
				}
				continue
			}
			*moveList = append(*moveList, move)
		}
	}
}

func (b *Board) pawnCaptureBitboards() (east uint64, west uint64) {
	notHFile := uint64(0x7F7F7F7F7F7F7F7F)
	notAFile := uint64(0xFEFEFEFEFEFEFEFE)
	var targets uint64
	if b.enpassant > 0 { // an en-passant target is active
		targets = (1 << b.enpassant)
	}
	if b.wtomove {
		targets |= b.black.all
		ourpawns := b.white.pawns
		east = ourpawns << 9 & notAFile & targets
		west = ourpawns << 7 & notHFile & targets
	} else {
		targets |= b.white.all
		ourpawns := b.black.pawns
		east = ourpawns >> 7 & notAFile & targets
		west = ourpawns >> 9 & notHFile & targets
	}
	return
}

func (b *Board) knightMoves(moveList *[]Move) {
	var ourKnights uint64
	var noFriendlyPieces uint64
	if b.wtomove {
		ourKnights = b.white.knights
		noFriendlyPieces = (^b.white.all)
	} else {
		ourKnights = b.black.knights
		noFriendlyPieces = (^b.black.all)
	}
	for ourKnights != 0 {
		currentKnight := bits.TrailingZeros64(ourKnights)
		ourKnights &= ourKnights - 1
		targets := knightMasks[currentKnight] & noFriendlyPieces
		genMovesFromTargets(moveList, Square(currentKnight), targets)
	}
}

// TODO: Can't castle from, into, or through check
// This assumes exactly one king is present
func (b *Board) kingMoves(moveList *[]Move) {
	var ourKingLocation uint8
	var noFriendlyPieces uint64
	var canCastleQueenside bool
	var canCastleKingside bool
	var ptrToOurBitboards *bitboards
	allPieces := b.white.all & b.black.all
	if b.wtomove {
		ourKingLocation = uint8(bits.TrailingZeros64(b.white.kings))
		ptrToOurBitboards = &(b.white)
		noFriendlyPieces = ^(b.white.all)
		// To castle, we must have rights and a clear path
		kingsideClear := allPieces&(1<<5)&(1<<6) == 0
		queensideClear := allPieces&(1<<3)&(1<<2)&(1<<1) == 0
		canCastleQueenside = b.whiteCanCastleQueenside() &&
			queensideClear && !b.anyUnderDirectAttack(true, 0, 1, 2, 3, 4)
		canCastleKingside = b.whiteCanCastleKingside() &&
			kingsideClear && !b.anyUnderDirectAttack(true, 4, 5, 6, 7)
	} else {
		ourKingLocation = uint8(bits.TrailingZeros64(b.black.kings))
		ptrToOurBitboards = &(b.black)
		noFriendlyPieces = ^(b.black.all)
		kingsideClear := allPieces&(1<<61)&(1<<62) == 0
		queensideClear := allPieces&(1<<57)&(1<<58)&(1<<59) == 0
		canCastleQueenside = b.blackCanCastleQueenside() &&
			queensideClear && !b.anyUnderDirectAttack(false, 56, 57, 58, 59, 60)
		canCastleKingside = b.blackCanCastleKingside() &&
			kingsideClear && !b.anyUnderDirectAttack(false, 60, 61, 62, 63)
	}
	if canCastleKingside {
		var move Move
		move.Setfrom(Square(ourKingLocation)).Setto(Square(ourKingLocation + 2))
		*moveList = append(*moveList, move)
	}
	if canCastleQueenside {
		var move Move
		move.Setfrom(Square(ourKingLocation)).Setto(Square(ourKingLocation - 2))
		*moveList = append(*moveList, move)
	}

	// TODO(dylhunn): Modifying the board is NOT thread-safe.
	// We only do this to avoid the king danger problem, aka moving away from a
	// checking slider.
	oldKings := ptrToOurBitboards.kings
	ptrToOurBitboards.kings = 0
	ptrToOurBitboards.all &= ^(1 << ourKingLocation)

	targets := kingMasks[ourKingLocation] & noFriendlyPieces
	for targets != 0 {
		target := bits.TrailingZeros64(targets)
		targets &= targets - 1
		if b.underDirectAttack(b.wtomove, uint8(target)) {
			continue
		}
		var move Move
		move.Setfrom(Square(ourKingLocation)).Setto(Square(target))
		*moveList = append(*moveList, move)
	}

	ptrToOurBitboards.kings = oldKings
	ptrToOurBitboards.all |= (1 << ourKingLocation)
}

func (b *Board) rookMoves(moveList *[]Move) {
	var ourRooks uint64
	var friendlyPieces uint64
	if b.wtomove {
		ourRooks = b.white.rooks
		friendlyPieces = b.white.all
	} else {
		ourRooks = b.black.rooks
		friendlyPieces = b.black.all
	}
	allPieces := b.white.all | b.black.all
	for ourRooks != 0 {
		currRook := bits.TrailingZeros64(ourRooks)
		ourRooks &= ourRooks - 1
		blockers := magicRookBlockerMasks[currRook] & allPieces
		dbindex := (blockers * magicNumberRook[currRook]) >> magicRookShifts[currRook]
		targets := magicMovesRook[currRook][dbindex] & (^friendlyPieces)
		genMovesFromTargets(moveList, Square(currRook), targets)
	}
}

func (b *Board) bishopMoves(moveList *[]Move) {
	var ourBishops uint64
	var friendlyPieces uint64
	if b.wtomove {
		ourBishops = b.white.bishops
		friendlyPieces = b.white.all
	} else {
		ourBishops = b.black.bishops
		friendlyPieces = b.black.all
	}
	allPieces := b.white.all | b.black.all
	for ourBishops != 0 {
		currBishop := bits.TrailingZeros64(ourBishops)
		ourBishops &= ourBishops - 1
		blockers := magicBishopBlockerMasks[currBishop] & allPieces
		dbindex := (blockers * magicNumberBishop[currBishop]) >> magicBishopShifts[currBishop]
		targets := magicMovesBishop[currBishop][dbindex] & (^friendlyPieces)
		genMovesFromTargets(moveList, Square(currBishop), targets)
	}
}

func (b *Board) queenMoves(moveList *[]Move) {
	var ourQueens uint64
	var friendlyPieces uint64
	if b.wtomove {
		ourQueens = b.white.queens
		friendlyPieces = b.white.all
	} else {
		ourQueens = b.black.queens
		friendlyPieces = b.black.all
	}
	allPieces := b.white.all | b.black.all
	for ourQueens != 0 {
		currQueen := bits.TrailingZeros64(ourQueens)
		ourQueens &= ourQueens - 1
		// bishop motion
		diag_blockers := magicBishopBlockerMasks[currQueen] & allPieces
		diag_dbindex := (diag_blockers * magicNumberBishop[currQueen]) >> magicBishopShifts[currQueen]
		diag_targets := magicMovesBishop[currQueen][diag_dbindex] & (^friendlyPieces)
		genMovesFromTargets(moveList, Square(currQueen), diag_targets)
		// rook motion
		ortho_blockers := magicRookBlockerMasks[currQueen] & allPieces
		ortho_dbindex := (ortho_blockers * magicNumberRook[currQueen]) >> magicRookShifts[currQueen]
		ortho_targets := magicMovesRook[currQueen][ortho_dbindex] & (^friendlyPieces)
		genMovesFromTargets(moveList, Square(currQueen), ortho_targets)
	}
}

// Helper: converts a targets bitboard into moves, and adds them to the list
func genMovesFromTargets(moveList *[]Move, origin Square, targets uint64) {
	for targets != 0 {
		target := bits.TrailingZeros64(targets)
		targets &= targets - 1
		var move Move
		move.Setfrom(origin).Setto(Square(target))
		*moveList = append(*moveList, move)
	}
}

func (b *Board) anyUnderDirectAttack(byBlack bool, squares ...uint8) bool {
	for _, v := range squares {
		if b.underDirectAttack(byBlack, v) {
			return true
		}
	}
	return false
}

func (b *Board) underDirectAttack(byBlack bool, origin uint8) bool {
	allPieces := b.white.all | b.black.all
	var opponentPieces *bitboards
	if byBlack {
		opponentPieces = &(b.black)
	} else {
		opponentPieces = &(b.white)
	}
	// find attacking knights
	knight_attackers := knightMasks[origin] & opponentPieces.knights
	if knight_attackers != 0 {
		return true
	}
	// find attacking bishops and queens
	diag_candidates := magicBishopBlockerMasks[origin] & allPieces
	diag_dbindex := (diag_candidates * magicNumberBishop[origin]) >> magicBishopShifts[origin]
	diag_potential_attackers := magicMovesBishop[origin][diag_dbindex] & opponentPieces.all
	diag_attackers := diag_potential_attackers & (opponentPieces.bishops | opponentPieces.queens)
	if diag_attackers != 0 {
		return true
	}
	// find attacking rooks and queens
	ortho_candidates := magicRookBlockerMasks[origin] & allPieces
	ortho_dbindex := (ortho_candidates * magicNumberRook[origin]) >> magicRookShifts[origin]
	ortho_potential_attackers := magicMovesRook[origin][ortho_dbindex] & opponentPieces.all
	ortho_attackers := ortho_potential_attackers & (opponentPieces.rooks | opponentPieces.queens)
	if ortho_attackers != 0 {
		return true
	}
	// find attacking kings
	// TODO(dylhunn): What if the opponent king can't actually move to the origin square?
	king_attackers := kingMasks[origin] & opponentPieces.kings
	if king_attackers != 0 {
		return true
	}

	// find attacking pawns
	var pawn_attackers uint64 = 0
	if byBlack {
		pawn_attackers = 1 << (origin + 7)
		pawn_attackers |= 1 << (origin + 9)
	} else {
		if origin-7 >= 0 {
			pawn_attackers = 1 << (origin - 7)
		}
		if origin-9 >= 0 {
			pawn_attackers |= 1 << (origin - 9)
		}
	}
	pawn_attackers &= opponentPieces.pawns
	if pawn_attackers != 0 {
		return true
	}
	return false
}
