# git-email

[English](README.md) | 日本語

`git-email` は、GitHub ユーザーが所有するリポジトリの Git 履歴を調べ、メールアドレスが含まれているリポジトリを表示する Go CLI です。通常の検査は読み取り専用です。明示的に確認した場合だけ、commitのauthor/committerメールを置換して履歴をforce-pushできます。

- public/private を含む本人所有リポジトリを列挙
- 全 branch/tag から到達可能な commit author/committer を検査
- 現在および過去の全 Git blob を検査
- 指定メールの完全一致検索、またはメールの自動検出
- 人間向け text と機械処理向け JSON 出力
- オプションで対象commitメールを認証ユーザーのGitHub noreplyアドレスへ置換

検査のためにリポジトリを一時 mirror として取得しますが、取得物は実行終了時にローカルから後片付けされます。

## 必要環境

- Go 1.25 以上
- Git
- private リポジトリの検査または履歴書き換えを行う場合は GitHub Personal Access Token

## ビルド

```bash
go build -o git-email ./cmd/git-email
```

## Token の準備

public/private を漏れなく検査するには、GitHub の fine-grained personal access token を次の設定で作成します。

- Resource owner: 検査する本人
- Repository access: All repositories
- Repository permissions:
  - Metadata: Read（自動付与）
  - Contents: Read-only

commitを書き換える場合は、追加で`Contents: Read and write`が必要です。Repository rulesとbranch protectionで、認証ユーザーによる対象refへのforce-pushが許可されている必要があります。

Token はコマンド引数や clone URL に入れず、`GITHUB_TOKEN` 環境変数で渡してください。

```bash
read -s GITHUB_TOKEN
export GITHUB_TOKEN
```

ツールは認証ユーザー名と指定 owner が一致すること、および API で列挙できた public/private repo 数がアカウントの件数と一致することを確認します。不一致の場合は結果を表示したうえで「検査不完全」として終了します。

## 使い方

### 指定メールを検索

`--email` は複数回指定できます。比較時は大文字小文字を区別しません。

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan \
  --email user@example.com \
  james-yusuke
```

GitHub profile URL も指定できます。

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan \
  --email user@example.com \
  https://github.com/james-yusuke
```

### メールを自動検出

`--email` を省略すると、メール形式の文字列をすべて検出します。`users.noreply.github.com` などの GitHub noreply アドレスは自動的に除外されます。

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan james-yusuke
```

自動検出は、README に意図的に掲載した問い合わせ先なども候補として表示します。特定の個人メールだけを調べたい場合は `--email` を指定してください。

### public repo のみ検査

```bash
./git-email scan --public-only james-yusuke
```

### JSON 出力

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan \
  --format json \
  --email user@example.com \
  james-yusuke
```

### 並列数の変更

既定では最大4リポジトリを並列に検査します。

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan --jobs 2 james-yusuke
```

### 対象commitメールを書き換える

> [!CAUTION]
> この操作はGit履歴を書き換え、変更されたbranch/tagをforce-pushします。commit SHAが変わり、書き換え対象履歴のcommit/tag署名は無効になります。共同作業者はrebaseまたは再cloneが必要です。fork、キャッシュ済みcommitページ、外部コピーには元のメールが残る可能性があります。

次のすべてを指定しない限り、書き換えは実行されません。

- 1件以上の明示的な`--email`（自動検出結果は書き換えに使用できません）
- `--rewrite-commits`
- 破壊的操作を確認する`--yes`
- 指定owner本人の、全対象repoへの書き込み権限を持つToken

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan \
  --email user@example.com \
  --rewrite-commits \
  --yes \
  james-yusuke
```

ファイルツリー、commit message、名前、日時は保持されます。一致するauthor/committerメールだけを`ID+USERNAME@users.noreply.github.com`へ置換します。hashが変わったbranch/tagだけを、同時更新を上書きしないatomicな`--force-with-lease`でpushします。ファイル本文から見つかったメールは表示のみで、変更しません。

## 表示の意味

```text
EXPOSED https://github.com/example/public-repo
  email: person@example.com
  visibility: public
  sources: blob, commit_author
  matches: 3
```

- `EXPOSED`: public repo 内で検出され、外部から参照可能
- `PRIVATE_FINDING`: private repo 内で検出されたが、外部公開済みとは断定しない
- `commit_author`: commit author のメール
- `commit_committer`: commit committer のメール
- `blob`: Git が管理するファイル内容
- `REWRITTEN`: commit metadataの置換と表示されたrefのforce-pushが成功

同じメールはリポジトリ単位で集約し、検出数と最大5件の代表的な SHA/path を表示します。ファイルの本文は表示しません。

## 終了コード

| Code | 意味 |
| ---: | --- |
| `0` | メールを検出せず、検査が完了した |
| `1` | 1件以上のメールを検出した |
| `2` | 認証、権限、API、clone などの問題で検査が不完全だった |

検出結果とエラーが両方ある場合は、結果を表示して終了コード `2` を返します。

書き換えに成功した場合も、最初の検査でメールを検出した記録を返すため終了コードは`1`です。書き換えまたはpushに失敗した場合は`2`です。

## 検査対象外

- branch/tag のどちらからも到達できない Git オブジェクト
- Git LFS の実ファイル（Git 内の LFS pointer は検査対象）
- 圧縮ファイルや暗号化ファイルの展開後の内容
- Issue、Pull Request コメント、Wiki、Release、Actions log
- submodule が参照する外部リポジトリの内容

## セキュリティ

- GitHub API は GET のみ使用します。
- 通常の検査は読み取り専用のGit操作だけを使用し、pushや履歴変更を行いません。
- force-pushは`--rewrite-commits --yes --email ...`を組み合わせた場合だけ有効になります。
- Token は Git の引数、clone URL、レポート、エラーメッセージに含めません。
- private repo を含む一時 mirror は、成功・失敗・キャンセル時のいずれも実行終了時に後片付けします。

## 開発時の確認

```bash
go test -race ./...
go vet ./...
go build ./cmd/git-email
```
