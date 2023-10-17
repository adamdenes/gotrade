package backtest

import "github.com/adamdenes/gotrade/internal/models"

type Engine interface {
	Run()
	Init()
	Reset()
}

type Strategy interface {
	Execute()
	SetData([]*models.KlineSimple)
}
