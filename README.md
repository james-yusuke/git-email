# git-email

`git-email` は、GitHub ユーザーが所有するリポジトリの Git 履歴を調べ、メールアドレスが含まれているリポジトリを表示する読み取り専用の Go CLI です。

- public/private を含む本人所有リポジトリを列挙
- 全 branch/tag から到達可能な commit author/committer を検査
- 現在および過去の全 Git blob を検査
- 指定メールの完全一致検索、またはメールの自動検出
- 人間向け text と機械処理向け JSON 出力
- GitHub 上のリポジトリ、履歴、ファイルを変更・削除しない

検査のためにリポジトリを一時 mirror として取得しますが、取得物は実行終了時にローカルから後片付けされます。

## 必要環境

- Go 1.25 以上
- Git
- private リポジトリを含める場合は GitHub Personal Access Token

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

同じメールはリポジトリ単位で集約し、検出数と最大5件の代表的な SHA/path を表示します。ファイルの本文は表示しません。

## 終了コード

| Code | 意味 |
| ---: | --- |
| `0` | メールを検出せず、検査が完了した |
| `1` | 1件以上のメールを検出した |
| `2` | 認証、権限、API、clone などの問題で検査が不完全だった |

検出結果とエラーが両方ある場合は、結果を表示して終了コード `2` を返します。

## 検査対象外

- branch/tag のどちらからも到達できない Git オブジェクト
- Git LFS の実ファイル（Git 内の LFS pointer は検査対象）
- 圧縮ファイルや暗号化ファイルの展開後の内容
- Issue、Pull Request コメント、Wiki、Release、Actions log
- submodule が参照する外部リポジトリの内容

## セキュリティ

- GitHub API は GET のみ使用します。
- `git clone --mirror`、`git rev-list`、`git cat-file` のみ使用し、push や履歴変更コマンドは実行しません。
- Token は Git の引数、clone URL、レポート、エラーメッセージに含めません。
- private repo を含む一時 mirror は、成功・失敗・キャンセル時のいずれも実行終了時に後片付けします。

## 開発時の確認

```bash
go test -race ./...
go vet ./...
go build ./cmd/git-email
```
