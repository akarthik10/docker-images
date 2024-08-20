#!/bin/sh

gosu sogo /usr/local/sbin/sogod -WONoDetach YES -WOPidFile /var/run/sogo/sogo.pid -WOLogFile - &