#!/bin/bash

echo "Остановка приложения DineBook..."
pkill -f dinebook-go

if [ $? -eq 0 ]; then
    echo "Приложение успешно остановлено"
else
    echo "Приложение не было запущено"
fi 