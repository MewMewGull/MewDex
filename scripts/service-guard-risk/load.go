//go:build darwin

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const loadWaves, loadWorkers, loadPerWorker = 32, 8, 8

type loadResult struct {
	Waves     int   `json:"waves"`
	Workers   int   `json:"workers"`
	Completed int   `json:"completed"`
	ElapsedMS int64 `json:"elapsedMs"`
}

func fixtureTimeout(root string) time.Duration {
	if filepath.Base(root) == "concurrent-churn" {
		return 90 * time.Second
	}
	return 18 * time.Second
}

func concurrentChurn(root string) error {
	started := time.Now()
	for wave := 0; wave < loadWaves; wave++ {
		var workers sync.WaitGroup
		failures := make(chan error, loadWorkers)
		for worker := 0; worker < loadWorkers; worker++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for task := 0; task < loadPerWorker; task++ {
					if _, err := os.Stat(filepath.Join(root, "stop")); err == nil {
						failures <- errors.New("the private fixture was stopped")
						return
					}
					if err := exec.Command("/usr/bin/true").Run(); err != nil {
						failures <- err
						return
					}
				}
			}()
		}
		workers.Wait()
		close(failures)
		for err := range failures {
			return err
		}
		progress := loadResult{Waves: wave + 1, Workers: loadWorkers, Completed: (wave + 1) * loadWorkers * loadPerWorker, ElapsedMS: time.Since(started).Milliseconds()}
		if err := save(filepath.Join(root, "concurrent-churn.progress.json"), progress); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return save(filepath.Join(root, "concurrent-churn.done.json"), loadResult{Waves: loadWaves, Workers: loadWorkers, Completed: loadWaves * loadWorkers * loadPerWorker, ElapsedMS: time.Since(started).Milliseconds()})
}
