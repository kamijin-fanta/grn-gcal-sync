# grn-gcal-sync

サイボウズのグループウェアGaroonに登録されたスケジュールをGoogleカレンダーに同期するためのアプリケーションです。Googleカレンダーを通じて様々なアプリケーション・通知方法で予定を確認することが出来るようになります。Garoonはオンプレ・クラウド版両方に対応しているつもりです。

定期的に実行するためのサーバをVMから用意するのは手間なので、GitHub Actionsでの運用を推奨しています。実行に必要なDocker Imageの配布はこのリポジトリのPackagesから行っています。

こちらのブログ記事がきっかけで公開したものです: https://blog.kamijin-fanta.info/2020/09/dont-forget-meeting/

## 設定方法

### 同期先のカレンダーを用意する

Garoonに存在しない予定は削除されてしまうので、Googleカレンダーにて新規のカレンダーを作成します。
Googleカレンダーの「設定」から「カレンダーを追加」、作成したカレンダーのカレンダーIDをメモしておきます。
（例: vhogehoge@group.calendar.google.com ）

### GitHubのプライベートリポジトリに設定を行う

GitHub Actionsを実行するためのリポジトリ・資格情報・設定等を用意します。

GitHubで作成したプライベートリポジトリへ、 `.env` ファイルを作成して以下のような設定を記述します。
uidはGaroonの自分のプロフィールページのURLパラメータ `uid` から参照可能です。

```env
# .env

GAROON_USER=xxxxx    # ログインするためのユーザ名
GAROON_PASS=xxxxx    # ログインするためのパスワード・後で消す
GAROON_USER_ID=xxxxx # Garoonでのuidを指定
GAROON_URL=https://example.com/scripts/cbgrn/grn.exe # オンプレ環境ならURLを指定
GAROON_LINK_BASE=https://example.com/scripts/cbgrn/grn.exe
GCAL_ID=vhogehoge@group.calendar.google.com
```

token.jsonを生成します。

```
$ touch token.json # 空ファイル作成。
$ docker run -it --env-file=.env -v $PWD/token.json:/token.json docker.pkg.github.com/kamijin-fanta/grn-gcal-sync/grn-gcal-sync:v1.1.0 /grn-gcal-sync --gcal-token-path=token.json sync
# ブラウザで出力されるリンクを開いて、Google認証後のトークンをコピペして、端末に戻ってきて貼り付ける。途中安全でないとかの警告がでるが無視して進める。
```

GitHubのSecret機能でGAROON_PASS変数の値を暗号化します。
作成したリポジトリの`Settings`から`Secrets`にて設定を行い、`.env`ファイルから`GAROON_PASS`を削除します。

[https://docs.github.com/en/free-pro-team@latest/actions/reference/encrypted-secrets](https://docs.github.com/en/free-pro-team@latest/actions/reference/encrypted-secrets)

`.github/workflows/cron.yaml` を作成します。
YAMLの中身は以下のままコピペしてください。

```yaml
name: schedule
on:
  schedule:
    - cron: '0 0-10 * * 1-5' # At minute 0 past every hour from 9 through 19 on every day-of-week from Monday through Friday.
  push:
jobs:
  run_sync:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@master
    - name: login
      run: echo ${GITHUB_TOKEN} | docker login -u ${GITHUB_ACTOR} --password-stdin docker.pkg.github.com
      env:
        GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    - name: build and push
      run: |
        docker run --env GAROON_PASS='${{ secrets.GAROON_PASS }}' --env-file=.env -v $PWD/token.json:/token.json docker.pkg.github.com/kamijin-fanta/grn-gcal-sync/grn-gcal-sync:v1.1.0 /grn-gcal-sync --gcal-token-path=token.json sync
```

ここまでのファイルを全てcommit,pushします。

### 動作確認

GitHubリポジトリのActionsタブから動作している様子がみえます。
平日の営業時間中に1時間に1回実行されます。
ジョブが失敗すればGitHubへ登録しているメールアドレスへ通知されます。
