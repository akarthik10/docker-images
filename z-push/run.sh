#!/bin/sh

sed -i "/TIMEZONE/c\    define('TIMEZONE', '"$TIMEZONE"');" /usr/local/lib/z-push/config.php
sed -i "/'STATE_DIR'/c\    define('STATE_DIR', '/state/');" /usr/local/lib/z-push/config.php

sed -i "/IMAP_SERVER/c\ define('IMAP_SERVER', '"$IMAP_SERVER"');" /usr/local/lib/z-push/backend/imap/config.php
sed -i "/IMAP_PORT/c\ define('IMAP_PORT', '"$IMAP_PORT"');" /usr/local/lib/z-push/backend/imap/config.php 
sed -i "/'IMAP_SMTP_METHOD'/c\ define('IMAP_SMTP_METHOD', 'smtp');" /usr/local/lib/z-push/backend/imap/config.php
sed -i "/\$imap_smtp_params = array()/c\ \$imap_smtp_params = array('host' => '"$SMTP_SERVER"', 'port' => '"$SMTP_PORT"', 'auth' => true, 'username' => 'imap_username', 'password' => 'imap_password', 'verify_peer' => true, 'verify_peer_name' => true, 'allow_self_signed' => true, 'debug' => true);" /usr/local/lib/z-push/backend/imap/config.php
sed -i "/IMAP_FOLDER_CONFIGURED/c\ define('IMAP_FOLDER_CONFIGURED', true);" /usr/local/lib/z-push/backend/imap/config.php

/usr/bin/supervisord -n
