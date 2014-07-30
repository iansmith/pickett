package io

import (
	"fmt"
	"math/rand"
	"time"
)

var DIE = []string{
	"dead",
	"deceased",
	"passed",
	"strangled",
	"poisoned",
	"murdered",
	"overdosed",
	"killed",
	"bumpedoff",
	"fell",
	"drowned",
	"shot",
	"stabbed",
	"crushed",
	"burned",
	"immolated",
	"fried",
	"electrocuted",
	"hung",
	"slipped",
	"crashed",
}

var STAR = []string{
	"elvis",
	"lennon",
	"hendrix",
	"joplin",
	"holly",
	"cobain",
	"vicious",
	"curtis",
	"garcia",
	"moon",
	"cash",
	"morrisson",
	"ramone",
	"entwhistle",
	"valens",
	"bono",
	"cook",
	"epstein",
	"jones",
	"croce",
	"moon",
	"bonham",
	"haley",
	"wilson",
	"marley",
	"hutchence",
	"perkins",
	"falco",
	"strummer",
	"gibb",
	"zevon",
	"brown",
	"mca",
	"winehouse",
	"manzarek",
}

var wordRand = rand.New(rand.NewSource(time.Now().UnixNano()))

func newPhrase() string {
	dead := wordRand.Intn(len(DIE))
	star := wordRand.Intn(len(STAR))
	combo := fmt.Sprintf("%s_%s", DIE[dead], STAR[star]) //on our hands, at last, a dead star...
	return combo
}
