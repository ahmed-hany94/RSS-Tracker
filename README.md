# RSS Feed Tracker

A lightweight, concurrent RSS/Atom feed tracker written in Go that monitors your favorite blogs and websites for new content.

## Local Database

A file with a format similar to [`dummy.json`](./dummy.json) that stores each site's feed url and the latest entry.

## Running

Check the latest entries and compare them to ones in our database.

```bash
$ ./main.exe
```

## Adding new Site


```bash
$ .\main.exe -a
Enter Site Name: Site Name
Enter Site RSS URL: https://example.com/atom
Testing feed... OK (Atom feed detected)\n
✓ Successfully added 'Site Name'\n

Add another site? (y/n): y
...
```
