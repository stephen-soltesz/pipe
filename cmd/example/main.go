package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/m-lab/go/logx"

	"github.com/google/go-github/github"
	"github.com/m-lab/alertmanager-github-receiver/issues"
	"github.com/m-lab/go/rtx"
	"github.com/stephen-soltesz/pipe/shx"
)

/*
pipe
shell
shx
compose
gosh
script

ops.Sh
cmd.Sh
script
command / operation / task / job / jx
hybrid shell go program script


shx.Task

shx.Func
shx.System
shx.Exec
shx.Pipe
shx.Script
*/

func init() {
	log.SetFlags(log.LUTC | log.Lshortfile)
}

func allrepos(ctx context.Context) ([]*github.Repository, error) {

	client := issues.NewClient("m-lab", "77aa5628c46316604fd8c588aea29746f898afed", "")

	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 10},
	}
	// get all pages of results
	var allRepos []*github.Repository
	for {
		repos, resp, err := client.GithubClient.Repositories.ListByOrg(ctx, "m-lab", opt)
		if err != nil {
			return nil, err
		}
		allRepos = append(allRepos, repos...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return allRepos, nil
}

func dirMissing(dirname string) shx.Task {
	return shx.Func(
		fmt.Sprintf("test -d %s", dirname),
		func(ctx context.Context, s *shx.State) error {
			if _, err := os.Stat(s.Path(dirname)); os.IsNotExist(err) {
				logx.Debug.Printf("! test -d %s -> true", s.Path(dirname))
				return nil
			}
			logx.Debug.Printf("! test -d %s -> false", s.Path(dirname))
			return fmt.Errorf("dir exists:" + s.Path(dirname))
		},
	)
}

func splitDateCommit(lr *bufio.Reader) (string, string, error) {
	line, err := lr.ReadString('\n')
	if err == io.EOF {
		return "", "", err
	}
	fields := strings.Split(line[:len(line)-1], " ")
	date, commit := fields[0], fields[1]
	return date, commit, nil
}

func dateAdd(d string, n int) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		rtx.Must(err, "failed to parse d: %s", d)
		return d
	}
	t = t.AddDate(0, 0, 1)
	return t.Format("2006-01-02")
}

func eofErr(err error) error {
	if err == io.EOF {
		return nil
	}
	return err
}

func FuncForEachLine(run func(line string) error) shx.Task {
	return shx.Func(
		"foreachline",
		func(ctx context.Context, s *shx.State) error {
			lr := bufio.NewReader(s.Stdin)
			line, err := lr.ReadString('\n')
			for err == nil && ctx.Err() == nil {
				err = run(line[:len(line)-1])
				if err != nil {
					break
				}
				line, err = lr.ReadString('\n')
			}
			if err == io.EOF {
				return nil
			}
			return err
		},
	)
}

func dateSequence(end string) shx.Task {
	return shx.Func(
		"date sequence",
		func(ctx context.Context, s *shx.State) error {
			lr := bufio.NewReader(s.Stdin)
			var date, commit string
			logx.Debug.Println("date sequence split")
			datePrev, commitPrev, err := splitDateCommit(lr)
			logx.Debug.Println("date sequence split:", datePrev, commitPrev, err)
			for err == nil {
				date, commit, err = splitDateCommit(lr)
				if err != nil {
					logx.Debug.Println("sequence err:", err)
					break
				}
				once := true
				for datePrev < date {
					//logx.Debug.Println("date commit:", datePrev, commitPrev)
					changed := fmt.Sprintf(" %t", commit != commitPrev && once)
					once = false
					_, err = s.Stdout.Write([]byte(datePrev + " " + commitPrev + changed + "\n"))
					if err != nil {
						logx.Debug.Println("write err:", err)
						return err
					}
					datePrev = dateAdd(datePrev, 1)
				}
				//logx.Debug.Println("date commit:", date, commit)
				changed := fmt.Sprintf(" %t", commit != commitPrev)
				_, err = s.Stdout.Write([]byte(date + " " + commit + changed + "\n"))
				if err != nil {
					logx.Debug.Println("write err:", err)
					return err
				}
				commitPrev = commit
				datePrev = date
			}
			for datePrev < end {
				//logx.Debug.Println("date commit:", datePrev, commitPrev)
				_, err = s.Stdout.Write([]byte(datePrev + " " + commitPrev + " false" + "\n"))
				if err != nil {
					logx.Debug.Println("write err:", err)
					return err
				}
				datePrev = dateAdd(datePrev, 1)
			}
			// logx.Debug.Println("date sequence split return:", err)
			return eofErr(err)
		})
}

func cloc(ctx context.Context, dir string) {
	s := &shx.State{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Dir:    dir,
	}
	commits := shx.Script(
		shx.System("git checkout master || git checkout main || :"),
		shx.Pipe(
			shx.System("git log --reverse --format='format:%cI %H'"),
			shx.System("tr 'T' ' '"),
			shx.System(`awk 'BEGIN {n=""} {if (n!= $1) { print $1, $3; n=$1}}'`),
			shx.WriteFile("commits.list", 0777),
		),
	)
	err := commits.Run(ctx, s)
	rtx.Must(err, "failed to collect commit list")

	s = &shx.State{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Dir:    dir,
		Env:    os.Environ(),
	}
	os.Chdir(dir)
	repo := path.Base(dir)
	// basedir := "/Users/soltesz/src/github.com/stephen-soltesz/pipe"
	runcloc := shx.Script(
		shx.SetEnv("repo", repo),
		shx.SetEnv("outdir", "/Users/soltesz/src/github.com/stephen-soltesz/pipe/summary"),
		shx.Pipe(
			shx.ReadFile("commits.list"),
			dateSequence("2021-03-01"),
			FuncForEachLine(func(line string) error {
				fields := strings.Split(line, " ")
				if len(fields) != 3 {
					return errors.New("wrong field count != 3:" + line)
				}
				date := fields[0]
				commit := fields[1]
				changed := fields[2]
				// logx.Debug.Println(line)

				t := []shx.Task{}
				if changed == "true" {
					t = append(t, shx.System("git checkout "+commit+
						"; cloc --read-lang-def=$HOME/out.langs --json . | jq . > out.json"))
					t = append(t, shx.System(
						"if [[ $( stat -f '%z' out.json ) -eq 0 ]] ; then echo '{}' > out.json ; fi"))
				}
				cmd := fmt.Sprintf(
					"jsonnet -J . --string --ext-str date=%s --ext-str repo=%s "+
						"../../out.jsonnet > ../../summary/%s-%s-summary.csv",
					date, repo, date, repo,
				)
				t = append(t, shx.System(cmd))
				return shx.Run(shx.Script(t...))
			}),
		),
	)
	err = runcloc.Run(ctx, s)
	rtx.Must(err, "failed to collect commit list")
}

func clone(ctx context.Context, lines []string) {
	d, err := os.Getwd()
	rtx.Must(err, "failed")

	s := &shx.State{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Dir:    path.Join(d, "m-lab"),
		// Env:    []string{},
	}

	for i := range lines {
		dirname := filepath.Base(lines[i])
		l := shx.Script(
			shx.IfThen(
				dirMissing(dirname),
				shx.System("git clone "+lines[i]+" "+dirname),
			),
		)
		fmt.Println(l.String())
		err := l.Run(ctx, s)
		if err != nil {
			log.Println("err:", err)
			break
		}
		logx.Debug.Println(path.Join(d, "m-lab", dirname))
		cloc(ctx, path.Join(d, "m-lab", dirname))
		time.Sleep(time.Second)
		logx.Debug.Println("return value")
		// return
	}
}

func main() {
	flag.Parse()
	fmt.Println("before")

	// ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	ctx := context.Background()
	// defer cancel()

	if _, err := os.Stat("repos.list"); os.IsNotExist(err) {
		logx.Debug.Println("creating repos.list")
		repos, err := allrepos(ctx)
		rtx.Must(err, "failed to list all repos")

		buf := &bytes.Buffer{}
		for i := range repos {
			fmt.Fprintln(buf, repos[i].GetCloneURL())
		}
		err = ioutil.WriteFile("repos.list", buf.Bytes(), 0777)
		rtx.Must(err, "failed to write repos.list")
	}
	logx.Debug.Println("reading repos.list")
	b, err := ioutil.ReadFile("repos.list")
	rtx.Must(err, "failed to read repos")
	lines := strings.Split(string(b), "\n")

	clone(ctx, lines)

	// time.Sleep(60 * time.Second)
	/*
		fmt.Println("goroutines:", runtime.NumGoroutine())
		cmd := exec.Command("lsof", "-p", fmt.Sprintf("%d", os.Getpid()))
		b, err := cmd.CombinedOutput()
		rtx.Must(err, "failed")
		fmt.Println(string(b))
		fmt.Println("goroutines:", runtime.NumGoroutine())
	*/
}
